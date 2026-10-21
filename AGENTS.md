# AGENTS.md — Go Clinic Booking System

> Directive for the development agent. Read this file completely before writing any code.
> Every rule below is a **decision**, not a suggestion. Where the original take-home brief was
> ambiguous, this file resolves the ambiguity. Do not re-litigate a decision here — implement it,
> and record it in the README as a design decision with its rationale.

---

## 0. Objective & Scope

Build a clinic booking system in Go as a take-home assessment. The assessment has **four
sections** and all four are in scope. A working API with no README design doc, no deployment, and
no CI/CD is a **failing** submission.

| Section | Deliverable | Where it lives |
|---|---|---|
| 1. System Design | Models, components, key decisions, trade-offs | `README.md` §"System Design" |
| 2. API Implementation | The Go service + tests | this repo |
| 3. Deployment & CI/CD | Live public URL + pipeline | `Dockerfile`, `render.yaml`, `.github/workflows/` |
| 4. AI Reflection | Answers to the 4 reflection questions | `README.md` §"AI Reflection" |

**Scenario:** a small clinic with 5 doctors. Each doctor has set working hours and works in
30-minute slots. Patients view free slots for a doctor on a date, book one, cancel, or reschedule.
Booked slots must not be double-booked.

**Non-goals (explicitly out of scope — say so in the README):** authentication/authorization,
doctor & patient CRUD write endpoints, pagination, email/SMS notifications, recurring
appointments, multi-tenant support.

---

## 1. Stack (decided — do not substitute)

| Concern | Choice | Rationale to put in the README |
|---|---|---|
| Language | Go 1.24+ (`go.mod`), toolchain 1.27 | Assessment permits Go. |
| Router | **stdlib `net/http`** | Go 1.22+ `ServeMux` natively supports `GET /doctors/{id}/availability` + `r.PathValue("id")`. Zero dependencies, no CVE surface. |
| DB | **PostgreSQL 18** via `database/sql` + `github.com/jackc/pgx/v5/stdlib` | The assessment allows any DB, but the concurrency requirements (partial unique index, real concurrent transactions) are Postgres-specific and worth demonstrating. |
| Migrations | Plain `.sql` files, `//go:embed`, applied idempotently on boot with a `schema_migrations` table | No CLI dependency; works identically in Docker, CI, and local dev. |
| Tests | stdlib `testing`, table-driven | |
| Deploy | **Render** (Docker runtime + managed Postgres) | Free tier, GitHub auto-deploy on merge, health checks, `PORT` injection. |
| CI | **GitHub Actions** | Test on every PR, deploy on merge to `main`. |

**No other third-party dependencies.** No gin, no chi, no ORM, no migration CLI, no assert
library. Standard library only, plus the Postgres driver.

Module path: `github.com/PhilN8/clinic-booking-system`.

---

## 2. Project Layout (fixed)

Do not put implementation code in `main.go`. Business logic does **not** live in handlers.

```
cmd/api/main.go                  # entrypoint: config -> db -> repos -> service -> router -> server
internal/config/config.go        # env parsing (PORT, DATABASE_URL, CLINIC_TZ, LOG_LEVEL)
internal/database/database.go    # connect, ping, migrate (embed migrations)
internal/database/migrations/    # 0001_init.sql, ... (embedded)
internal/httperr/httperr.go      # typed error + JSON error envelope
internal/models/models.go        # Doctor, Patient, Appointment
internal/repository/             # ALL SQL lives here
internal/repository/doctor.go
internal/repository/patient.go
internal/repository/appointment.go
internal/repository/errors.go    # ErrNotFound, ErrConflict, IsUniqueViolation
internal/service/                # ALL business logic lives here (pure, no SQL)
internal/service/service.go      # Service struct, booking/cancel/reschedule orchestration
internal/service/clock.go        # Clock interface + realClock
internal/service/validation.go   # pure validation functions
internal/service/slots.go        # slot grid generation
internal/service/*_test.go       # table-driven unit tests
internal/handlers/               # HTTP only: decode -> call service -> encode
internal/handlers/router.go
internal/handlers/appointments.go
internal/handlers/doctors.go
internal/handlers/patients.go
internal/handlers/handlers_test.go
.github/workflows/ci.yml
Dockerfile
docker-compose.yml
render.yaml
README.md
```

**Layering rule:** `handlers → service → repository → database`. The service layer never imports
`net/http`; handlers never build SQL; repositories contain no business rules.

---

## 3. Time & Slot Model (the crux of this system)

Get this right; most of the bugs in this kind of system live here.

- The clinic operates in **one fixed timezone**. Default `Africa/Nairobi` (UTC+03:00, **no DST**).
  Make it configurable via `CLINIC_TZ` env var, defaulting to `Africa/Nairobi`.
- Import `_ "time/tzdata"` in `main.go` so the binary is self-contained regardless of the base
  image's tzdata.
- Appointment times are **naive wall-clock times in the clinic timezone**. They are stored as
  `DATE` + `TIME`, never as `TIMESTAMPTZ`. Audit columns (`created_at`, `cancelled_at`) *are*
  `TIMESTAMPTZ` (absolute UTC) — this is intentional and must be documented.
- To compare a wall-clock appointment against "now", construct its instant with
  `time.Date(y, m, d, hh, mm, 0, 0, clinicLoc)` and compare against `clock.Now()`. This is exact
  because Africa/Nairobi has a fixed +03:00 offset and no DST. **Document this dependency in the
  README** — the wall-clock model is only safe because the clinic zone is DST-free and
  single-offset. If `CLINIC_TZ` is ever set to a zone with DST, this model needs revisiting.
- **Appointment duration is exactly 30 minutes.** It is a constant, not a column, not a
  request field. `end_time = start_time + 30 minutes`.
- **Slot grid:** the doctor's shift is divided into consecutive 30-minute slots anchored at the
  doctor's `start_working_hour`. A shift of 09:00–17:00 yields 16 slots: 09:00, 09:30, … 16:30
  (the last slot ends exactly at 17:00).
- **Working-hours bounds are inclusive**: `start_time >= shift_start` **and**
  `start_time + 30m <= shift_end`. A 09:00 start is valid; a 16:30 start is valid; a 17:00 start
  is not.
- **Grid alignment is mandatory.** `start_time` must land exactly on the grid
  (`(start_time - shift_start)` is a whole number of 30-minute steps). A 09:15 start is rejected.
  This is what makes the DB unique index sufficient to prevent double-booking — see §7.
- Doctors work **all 7 days of the week** with a single daily window. This is a deliberate
  simplification; list it under "Known limitations" in the README.
- **The 1-hour rule:** a booking is rejected if its start instant is **less than** 1 hour from
  now. A slot starting **exactly** 60 minutes from now is **allowed**. This rule subsumes the
  "not in the past" check. `GET /availability` applies the **same** predicate, so the API never
  advertises a slot that `POST` will refuse.

---

## 4. Database Schema

Exact DDL for migration `0001_init.sql`:

```sql
CREATE TABLE doctors (
    id                 BIGSERIAL PRIMARY KEY,
    name               TEXT NOT NULL,
    start_working_hour TIME NOT NULL,
    end_working_hour   TIME NOT NULL,
    CONSTRAINT doctors_hours_ordered
        CHECK (end_working_hour > start_working_hour),
    CONSTRAINT doctors_hours_on_half_hour
        CHECK (EXTRACT(MINUTE FROM start_working_hour) % 30 = 0
           AND EXTRACT(SECOND FROM start_working_hour)   = 0
           AND EXTRACT(MINUTE FROM end_working_hour)   % 30 = 0
           AND EXTRACT(SECOND FROM end_working_hour)     = 0)
);

CREATE TABLE patients (
    id   BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE appointments (
    id                  BIGSERIAL PRIMARY KEY,
    doctor_id           BIGINT NOT NULL REFERENCES doctors(id),
    patient_id          BIGINT NOT NULL REFERENCES patients(id),
    appointment_date    DATE   NOT NULL,
    start_time          TIME   NOT NULL,
    end_time            TIME   NOT NULL,
    status              TEXT   NOT NULL DEFAULT 'booked',
    cancellation_reason TEXT,
    cancelled_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    rescheduled_from_id BIGINT REFERENCES appointments(id),
    CONSTRAINT appointments_status_valid
        CHECK (status IN ('booked', 'cancelled')),
    CONSTRAINT appointments_duration_valid
        CHECK (end_time = start_time + INTERVAL '30 minutes')
);

-- Database-level double-booking protection.
CREATE UNIQUE INDEX appointments_no_double_booking
    ON appointments (doctor_id, appointment_date, start_time)
    WHERE status <> 'cancelled';

CREATE INDEX appointments_doctor_date_booked_idx
    ON appointments (doctor_id, appointment_date)
    WHERE status = 'booked';

CREATE INDEX appointments_patient_date_idx
    ON appointments (patient_id, appointment_date, start_time);
```

Notes to carry into the README:

- The **partial unique index** is the concurrency backstop. `status <> 'cancelled'` means a
  cancelled appointment frees its slot automatically — no cleanup job, no soft-delete bookkeeping.
- Because duration is fixed at 30 minutes **and** starts are grid-aligned, identical start times
  are the *only* possible overlap. The unique index is therefore sufficient; no `tstzrange`
  exclusion constraint / `btree_gist` extension is needed. Say this explicitly — it shows the
  constraint was reasoned about, not just copied.
- `status` is `TEXT` + `CHECK` rather than a Postgres `ENUM`, so adding a status later is a
  one-line migration.
- `rescheduled_from_id` links a replacement appointment to the one it replaced, so the
  cancel-then-insert lineage is queryable.

### Seed data

Migration `0002_seed.sql` inserts 5 doctors (09:00–17:00, 08:00–16:00, 10:00–18:00, 09:00–13:00,
13:00–17:00) and 2 patients with deterministic IDs. Migrations must be idempotent
(`ON CONFLICT DO NOTHING`) so re-running them is safe.

---

## 5. Validation Rules

All validation lives in `internal/service/validation.go` as **pure functions** that take plain
values and return a typed error. No DB, no HTTP, no globals. This is what makes the table-driven
tests in §8 possible.

`type Clock interface { Now() time.Time }` is injected into the service. Never call `time.Now()`
directly inside business logic — tests must be able to freeze it.

### POST /appointments — validation order (deterministic)

| # | Rule | Failure | Code |
|---|---|---|---|
| 1 | Body is valid JSON with no unknown fields | 400 | `INVALID_JSON` |
| 2 | `doctor_id`, `patient_id` present and > 0; `appointment_date` matches `2006-01-02`; `start_time` matches `15:04` (24h, `09:00`) | 400 | `INVALID_REQUEST` / `INVALID_DATE` / `INVALID_TIME` |
| 3 | Doctor exists | 404 | `DOCTOR_NOT_FOUND` |
| 4 | Patient exists | 404 | `PATIENT_NOT_FOUND` |
| 5 | `start_time >= shift_start` **and** `start_time + 30m <= shift_end` | 422 | `OUTSIDE_WORKING_HOURS` |
| 6 | `start_time` is on the doctor's 30-minute grid | 422 | `SLOT_NOT_ALIGNED` |
| 7 | `start instant >= now + 1h` (exactly 60 min away is **allowed**) | 422 | `TOO_SOON` |
| 8 | Slot not already booked | 409 | `SLOT_TAKEN` |

Rule 7 subsumes "must not be in the past". Rule 8 is enforced by the DB, not by a read-then-write
check — see §7.

**Rule 5 comes before rule 6 on purpose.** Checking the working window first produces the more
useful message: "08:30 when the doctor starts at 09:00" is reported as being outside working
hours, not as a misaligned start. An off-grid time that is *inside* the window (09:15) is still
reported as misaligned.

### PATCH /appointments/{id}/cancel

1. Body optional; `reason` is a string (nullable). A `appointment_id` in the body that conflicts
   with the path parameter → 400 `INVALID_REQUEST`.
2. Appointment exists → else 404 `APPOINTMENT_NOT_FOUND`.
3. Status is not already `cancelled` → else 409 `APPOINTMENT_ALREADY_CANCELLED`.
4. The appointment has not already started (`start instant >= now`) → else 422
   `APPOINTMENT_IN_PAST`. You cannot cancel a visit that already happened.
5. `UPDATE ... SET status='cancelled', cancellation_reason=$reason, cancelled_at=now()`.

Rules 2–4 are enforced by the **service layer**, which owns the business rules. The repository
re-checks rule 3 under an atomic `status = 'booked'` guard on the UPDATE, and re-checks rule 4
against the current clock, so the rule still holds if a request stalls between the two.

### PATCH /appointments/{id}/reschedule

Runs in **one atomic transaction**. Steps in order:

1. Parse `new_appointment_date` (`2006-01-02`) and `new_start_time` (`15:04`) → 400 on failure.
2. `BEGIN`.
3. `SELECT ... FROM appointments WHERE id=$1 FOR UPDATE` → 404 if missing. The row lock is
   **required**; without it two concurrent reschedules of the same appointment can both cancel it
   and insert two replacements.
4. Status is not `cancelled` → else 409 `APPOINTMENT_ALREADY_CANCELLED`.
5. The appointment has not started → else 422 `APPOINTMENT_IN_PAST`.
6. Apply validation rules 3–7 above to the **new** date/time (the doctor/patient are unchanged).
7. `UPDATE` the original: `status='cancelled'`, `cancellation_reason='rescheduled'`,
   `cancelled_at=now()`.
8. `INSERT` the new appointment with `status='booked'`, `rescheduled_from_id=<original id>`.
9. `COMMIT`.

Any error → `ROLLBACK`, and the original appointment remains `booked`. A unique-violation on the
insert → **409 `SLOT_TAKEN`**, not 500. The violation must be detected on the insert *inside* the
transaction, then surfaced after the rollback.

As with cancel, steps 4 and 5 are business rules and live in the service layer, which checks the
appointment's state before opening the transaction so that a cancelled appointment is reported as
a conflict rather than being masked by a validation error about the new slot. The repository
re-checks the same state under the row lock; that re-check is authoritative.

Rescheduling to the **same** slot is allowed (it cancels and rebooks the same slot; the partial
index permits this because the original leaves the index when it is cancelled). The doctor and
patient cannot be changed by a reschedule.

### GET /doctors/{id}/availability

1. `id` is a positive integer → else 400 `INVALID_REQUEST`.
2. `date` query param is **required** and must match `2006-01-02` → else 400 `INVALID_DATE`.
3. Doctor exists → else 404 `DOCTOR_NOT_FOUND`.
4. Generate the grid slots for that date from the doctor's shift.
5. Query `booked` appointments for that doctor + date → collect their start times.
6. Remove booked slots.
7. Remove slots whose start instant is `< now + 1h` (same predicate as POST).
8. Return the remaining slots.

A past date yields an empty slot list with **200**, not an error.

---

## 6. HTTP API Contract

Request bodies are JSON. Responses are JSON with `Content-Type: application/json`.
Unknown request fields are rejected (400 `INVALID_JSON`) rather than silently ignored.

### `POST /appointments` → **201 Created**

```json
{ "doctor_id": 1, "patient_id": 1, "appointment_date": "2026-10-22", "start_time": "09:30" }
```

```json
{
  "id": 1, "doctor_id": 1, "patient_id": 1,
  "appointment_date": "2026-10-22", "start_time": "09:30", "end_time": "10:00",
  "status": "booked", "cancellation_reason": null, "cancelled_at": null,
  "created_at": "2026-10-21T09:15:22Z", "rescheduled_from_id": null
}
```

### `GET /doctors/{id}/availability?date=2026-10-22` → **200 OK**

```json
{
  "doctor_id": 1,
  "date": "2026-10-22",
  "slots": [ { "start": "09:00", "end": "09:30" }, { "start": "09:30", "end": "10:00" } ]
}
```

### `PATCH /appointments/{id}/cancel` → **200 OK**

Request (reason optional but accepted and persisted):
`{ "reason": "Patient no longer needs the appointment" }`

Returns the updated appointment with `status: "cancelled"`, `cancellation_reason` set, and
`cancelled_at` set.

### `PATCH /appointments/{id}/reschedule` → **200 OK**

```json
{ "new_appointment_date": "2026-10-23", "new_start_time": "10:00" }
```

Returns the **new** appointment (status `booked`, new `id`, `rescheduled_from_id` set to the
original id).

### `GET /patients/{id}/appointments` → **200 OK** *(bonus)*

Upcoming appointments for a patient, sorted by `appointment_date, start_time` ascending.
Default: `status='booked'` and `start instant >= now`. Optional `?include_cancelled=true` adds
cancelled appointments (past and future) so the reschedule audit trail is visible.

```json
{ "patient_id": 1, "appointments": [ { "id": 1, "...": "..." } ] }
```

### `GET /doctors` and `GET /doctors/{id}` → **200 OK**

Read-only listing so a reviewer can discover IDs without reading the README. No write endpoints
for doctors/patients (see Non-goals).

### `GET /healthz` → **200 OK**

`{ "status": "ok" }`. Pings the database. Render uses this as its health check.

### Error envelope (all non-2xx responses)

```json
{
  "error": {
    "code": "OUTSIDE_WORKING_HOURS",
    "message": "start time 18:30 is outside the doctor's working hours (10:00-18:00)",
    "details": { "start_time": "18:30", "shift_start": "10:00", "shift_end": "18:00" }
  }
}
```

`message` is human-readable and must state **what was wrong**, not just restate the rule.
`details` is optional.

**Status code policy**

| Code | When |
|---|---|
| 200 / 201 | Success / resource created |
| 400 | Malformed JSON, unknown field, unparseable date/time, missing required param, bad path param |
| 404 | Doctor, patient, or appointment does not exist |
| 409 | Slot already booked, or appointment already cancelled |
| 422 | Semantically invalid but well-formed: outside working hours, off-grid start, inside the 1-hour window, appointment already started |

Document this table in the README. 400 = "I could not read your request"; 422 = "I read it and it
is not allowed".

**Error code strings:** `INVALID_JSON`, `INVALID_REQUEST`, `INVALID_DATE`, `INVALID_TIME`,
`DOCTOR_NOT_FOUND`, `PATIENT_NOT_FOUND`, `APPOINTMENT_NOT_FOUND`, `SLOT_NOT_ALIGNED`,
`OUTSIDE_WORKING_HOURS`, `TOO_SOON`, `APPOINTMENT_IN_PAST`, `SLOT_TAKEN`,
`APPOINTMENT_ALREADY_CANCELLED`, `INTERNAL_ERROR`.

---

## 7. Concurrency

The point of the exercise. Get it right.

- **Never** read-then-write to check availability. A "SELECT then INSERT" check has a race window.
  Attempt the `INSERT` and let the partial unique index be the arbiter; map the unique-violation
  to **409 `SLOT_TAKEN`**.
- Detect the violation with `pgconn.PgError` / `sql.ErrNoRows`-style inspection in the repository
  layer (`repository.IsUniqueViolation(err)`) and translate it to `repository.ErrConflict`. The
  handler maps `ErrConflict` → 409.
- Reschedule must lock the appointment row with `SELECT ... FOR UPDATE` inside the transaction.
- Cancelling and rescheduling must release the slot: the partial unique index only excludes rows
  whose status is `cancelled`, so flipping the status is sufficient.
- `GET /availability` is **advisory**. It can be stale by the time the client POSTs. `POST` is
  authoritative. Say this in the README.
- Use `db.SetMaxOpenConns` / `SetMaxIdleConns` / `SetConnMaxLifetime` in `internal/database`.

---

## 8. Testing

Stdlib `testing` only. **Table-driven** for every rule in §5.

**Unit tests (`internal/service/*_test.go`)** — target at minimum:

1. **Slot grid math** — 09:00–17:00 → 16 slots; 09:00–09:30 → 1 slot; first/last slot times.
2. **Working-hours boundaries** — start exactly at shift start → OK; end exactly at shift end → OK;
   one minute past → rejected; one minute before → rejected.
3. **Grid alignment** — 09:00 OK, 09:30 OK, 09:15 rejected, 08:30 rejected.
4. **The 1-hour rule** — exactly 60 minutes away → **allowed**; 59m59s → rejected; 30m → rejected;
   in the past → rejected. Use a frozen clock, never `time.Now()`.
5. **Date/time parsing** — `2026-02-30` rejected; `9:00` rejected; `09:00` accepted;
   `23:60` rejected.
6. **Error-to-status mapping** — each error code → the right HTTP status.

**Handler tests (`internal/handlers/handlers_test.go`)** — use `httptest`. Assert status code and
response body for each endpoint, including the error envelope shape.

**Integration tests** against a real PostgreSQL (skipped automatically when `TEST_DATABASE_URL` is
unset, so `go test ./...` still passes without a DB):

- Booking a taken slot → **409**, exactly one row survives.
- **Concurrent** double-booking of the same slot (two goroutines) → exactly one succeeds, one gets
  409. This is the headline test for the unique index.
- Cancel → the slot becomes bookable again.
- Reschedule → original is `cancelled`, new row is `booked`, `rescheduled_from_id` is set.
- Reschedule to an occupied slot → 409 **and the original is still `booked`** (rollback works).

**CI must run `go vet ./...` and `go test -race ./...`.**

---

## 9. Deployment & CI/CD

### `Dockerfile` — multi-stage

- Builder: `golang:1.27-alpine`, `CGO_ENABLED=0`, `go mod download` then `go build -o /out/api ./cmd/api`.
- Runtime: `alpine:3.20` (or `distroless`), non-root user, copy the binary only.
- `EXPOSE 8080`. The binary must listen on `PORT` (default `8080`).
- Graceful shutdown on `SIGINT`/`SIGTERM`: stop accepting connections, wait for in-flight
  requests, close the DB pool. Render sends `SIGTERM` on redeploy.
- `http.Server` must set `ReadHeaderTimeout` (e.g. 5s), `ReadTimeout`, `WriteTimeout`,
  `IdleTimeout`, and a `MaxHeaderBytes` cap. Body reads use `http.MaxBytesReader` (e.g. 1 MiB).

### `render.yaml`

Uses Render's current Blueprint format (`version: "1"`, `projects` → `environments` → `services`),
which is the shape Render exports from the dashboard. The flat `services:`/`databases:` layout and
the `projects:` layout are mutually exclusive — "do not define the same service in more than one
location".

- Service name: `clinic-booking-system-1`. Render **adopts** an existing service when the name
  matches exactly; a different name creates a second service.
- `runtime: docker`, `dockerfilePath: ./Dockerfile`, `dockerContext: .`
- `autoDeployTrigger: checksPass` — Render waits for the linked branch's CI checks to pass before
  it deploys, so the pipeline genuinely gates the release.
- `healthCheckPath: /healthz`
- The Postgres instance is declared in the same environment and `DATABASE_URL` is wired to it
  with `fromDatabase`.
- Migrations run in the application on boot under an advisory lock, not in a `preDeployCommand`:
  they complete before the HTTP server starts listening, so no request can reach the service with
  an outdated schema.

Validate the file against Render's published JSON Schema (`https://render.com/schema/render.yaml.json`)
or the Validate Blueprint API endpoint before relying on it.

### `.github/workflows/ci.yml`

- **On every pull request and push to `main`:** `go vet ./...`, `go test -race ./...` (with a
  `postgres:18` service container for the integration tests), `go build ./...`, and a validation
  of `render.yaml` against Render's Validate Blueprint endpoint.
- **On push to `main`:** build and push the Docker image to Docker Hub. The deploy itself is
  Render's `checksPass` trigger, so the pipeline does **not** also call the deploy hook — doing
  both would deploy twice.
- Every secret-dependent step is optional: if the secret is absent the step is skipped with a
  notice, so `main` never goes red on optional configuration.
- Document in the README: which branch triggers the deploy, what the pipeline does, and the live URL.

### `docker-compose.yml`

Local dev only: a `postgres:18` service with a named volume, a healthcheck, and the DSN that
matches `DATABASE_URL` in `.env.example`. Include a `.env.example`.

---

## 10. README (graded — do not skimp)

Required sections, in order:

1. **System Design** — entities, request flow, why the layering is what it is.
2. **Key Decisions & Trade-offs** — one subsection per decision, each with the alternative
   considered and why it was rejected. At minimum: PostgreSQL over SQLite; wall-clock clinic time
   over `TIMESTAMPTZ`; stdlib router over chi/gin; fixed 30-minute duration; partial unique index
   over application-level checks; cancel-and-reinsert for reschedule; no authentication.
3. **How to Run Locally** — `docker compose up -d`, `go run ./cmd/api`, and a curl walkthrough
   that actually works (copy-pasteable).
4. **API Reference** — every endpoint, with request/response examples and the status-code table.
5. **Testing** — how to run unit tests and how to run integration tests with a DB.
6. **Deployment** — the public URL, the deploy trigger branch, what the pipeline does.
7. **Known Limitations & Future Work** — the 7-day working week, no auth, no pagination, the
   DST-free-timezone assumption, no doctor/patient writes.
8. **AI Reflection** — answer all four assessment questions honestly and specifically. Name the
   prompt that produced a useful result, and name a case where the AI was wrong and how it was
   caught. Do not fake confidence; specificity is what's read.
9. **Design System** — point at `design.md` and explain that the colour tokens are enforced by
   `go test ./design/`.

---

## 10a. Design System

`design.md` is the design language for the patient-facing site. `design/tokens.json` is the
source of truth for colour and `design/tokens_test.go` enforces the WCAG 2.2 contrast
requirements for every documented pairing.

Rules for anyone extending it:

- **Never hardcode a hex in a component.** Reference a token.
- **A new colour goes in an existing scale.** A new hue needs a reason that survives the question
  "why is this not `brand.500` or `neutral.600`?"
- **A new pairing goes in `pairs` with its WCAG requirement.** If it cannot meet the
  requirement, the design is wrong, not the test.
- **Re-run `go test ./design/ -v` after any colour change** and regenerate the contrast table in
  `design.md`. That table is generated output and must never be hand-edited.
- **Available slots are neutral, not green** — a free slot is the default state. Colour marks
  state, never decoration. Red is reserved for errors and destructive actions; in a health
  context it means medical urgency and must not be used for a cancelled appointment.

---

## 11. Definition of Done

- [ ] `go vet ./...` clean, `go test -race ./...` green, `go build ./...` clean.
- [ ] All 4 required endpoints + 2 read endpoints + `/healthz` implemented per §6.
- [ ] Double-booking is impossible at the DB level; concurrent booking returns 409 (proven by test).
- [ ] Reschedule is atomic; a failed reschedule leaves the original `booked` (proven by test).
- [ ] Cancelled slots are bookable again (proven by test).
- [ ] The 1-hour rule is applied consistently by `POST` and `GET /availability`, and is unit-tested
      at the boundary.
- [ ] Table-driven tests cover every rule in §5.
- [ ] `Dockerfile`, `docker-compose.yml`, `render.yaml`, `.github/workflows/ci.yml` all present.
- [ ] The app is deployed and reachable at a public URL; `/healthz` returns 200.
- [ ] README contains all 8 sections from §10, including the live URL and the AI reflection.
- [ ] `curl` walkthrough in the README works verbatim from a clean clone.

---

## 12. Working Notes for the Agent

- **The assessment says "When requirements are ambiguous, make a decision and note it in your
  README. Clear reasoning matters as much as working code."** Ambiguity was resolved in this file
  — your job is to implement it and explain it, not to leave it unresolved.
- **The assessment says "We evaluate how you think, not just what you produce."** Comment the
  non-obvious reasoning inline (especially §3, §7) — a reviewer reading the diff should be able to
  see *why*.
- If you believe a decision in this file is wrong, **do not silently deviate**. Change it here in
  `AGENTS.md` and record the change and its rationale in the README's Key Decisions section.
- Go convention: idiomatic naming, `errors.Is`/`errors.As` for error handling, no `panic` outside
  `main`, table-driven tests with descriptive case names, `t.Helper()` in assertion helpers.
