# Clinic Booking System

A REST API for a small clinic with five doctors. Patients see which 30-minute slots are free for
a doctor on a given day, book one, cancel it, or move it to a different slot. A booked slot can
never be double-booked.

Built in Go with PostgreSQL. Standard library only, plus the Postgres driver.

The same binary also serves the **patient-facing site**, rendered on the server and styled from
the design tokens. Its design language is documented in [`design.md`](design.md), with the colour
tokens in [`design/tokens.json`](design/tokens.json) and their accessibility contrast ratios
enforced by the test suite.

| | |
|---|---|
| **Live site** | <https://clinic-booking-system-1-yu19.onrender.com/doctors> |
| **Live API** | <https://clinic-booking-system-1-yu19.onrender.com/api/doctors> |

---

## Table of contents

1. [System Design](#1-system-design)
2. [Key Decisions & Trade-offs](#2-key-decisions--trade-offs)
3. [How to Run Locally](#3-how-to-run-locally)
4. [API Reference](#4-api-reference)
5. [Testing](#5-testing)
6. [Deployment](#6-deployment)
7. [Known Limitations & Future Work](#7-known-limitations--future-work)
8. [AI Reflection](#8-ai-reflection)
9. [Design System](#9-design-system)

---

## 1. System Design

### 1.1 The scenario and the domain rules

Five doctors. Each has one working window a day, for example 09:00 to 17:00, divided into
sixteen 30-minute slots. A patient picks a doctor and a date, sees the free slots, and books
one. Bookings cannot overlap, and cancelling frees the slot again.

That reduces to three rules that the whole system is built around:

1. **A slot is valid** if it falls inside the doctor's working window, lands on the 30-minute
   grid, and is at least an hour away from now.
2. **A slot is taken** if a `booked` appointment exists for that doctor, date and start time.
3. **Cancelling and rescheduling release the slot** they occupied.

### 1.2 Entities

```
doctors
  id                 BIGSERIAL PK
  name               TEXT
  start_working_hour TIME
  end_working_hour   TIME

patients
  id   BIGSERIAL PK
  name TEXT

appointments
  id                  BIGSERIAL PK
  doctor_id           BIGINT FK -> doctors
  patient_id          BIGINT FK -> patients
  appointment_date    DATE
  start_time          TIME
  end_time            TIME
  status              TEXT   ('booked' | 'cancelled')
  cancellation_reason TEXT
  cancelled_at        TIMESTAMPTZ
  created_at          TIMESTAMPTZ
  rescheduled_from_id BIGINT FK -> appointments
```

`appointment_date`, `start_time` and `end_time` are **naive wall-clock values in the clinic's
timezone**. `created_at` and `cancelled_at` are **absolute instants** (`TIMESTAMPTZ`). The
distinction is deliberate: "the doctor works 09:00 to 17:00" is a local concept, while "when was
this row cancelled" is an event on a timeline.

### 1.3 Layering

```
HTTP request
    │
    ▼
internal/handlers      decode JSON, call the service, encode JSON
    │                  no business rules, no SQL
    ▼
internal/service       the clinic's rules: validity, grid, lead time, slot generation
    │                  pure functions, injected clock, no SQL
    ▼
internal/repository    every SQL statement and every transaction
    │
    ▼
PostgreSQL
```

The dependency arrow only points downwards. The service layer declares the persistence
interface it needs (`service.Repository`) and the Postgres implementation satisfies it
structurally, so the rules never depend on the database. That is what makes the rule tests
run without a database at all.

The rules live in the service layer because the rules *are* the product. Validation functions
take plain values and return typed errors:

```go
func ValidateSlot(doctor *models.Doctor, date, start time.Time, now time.Time, loc *time.Location) error
```

`now` is passed in rather than read from the clock inside, so a test can pin it to an exact
instant and assert on exact boundaries.

### 1.4 The slot model

A doctor's shift is divided into consecutive 30-minute slots anchored at the start of the shift:

```
09:00 shift start
09:00 ─┐
09:30  │
10:00  ├─ 16 slots
 ...   │
16:30 ─┘
17:00 shift end
```

Two properties fall out of this, and both matter:

- **Working-hours bounds are inclusive.** A 09:00 start is valid; a 16:30 start is valid
  because it ends exactly at 17:00; a 17:00 start is not.
- **Grid alignment is mandatory.** A 09:15 start is rejected. This is not cosmetic — it is what
  makes the database constraint below sufficient to prevent double-booking. With a fixed
  30-minute duration and aligned starts, two appointments can only overlap if they start at the
  *same* instant.

### 1.5 Preventing double-booking

The protection is a **partial unique index**:

```sql
CREATE UNIQUE INDEX appointments_no_double_booking
    ON appointments (doctor_id, appointment_date, start_time)
    WHERE status <> 'cancelled';
```

Three things follow from it:

- Two clients racing for one slot cannot both win. The second `INSERT` violates the index and
  is reported as `409 SLOT_TAKEN`.
- Cancelling frees the slot with **no cleanup job**: the cancelled row leaves the index because
  the index is partial.
- The application never needs a read-then-write check, which is where the classic race lives.
  The service attempts the insert and lets the database arbitrate.

The API never checks availability and then inserts. It inserts and lets the unique index decide.

### 1.6 Rescheduling atomically

A reschedule cancels the original and inserts a replacement. That has to be one transaction, and
it has to be serialised against other reschedules of the same appointment:

```
BEGIN
  SELECT ... FOR UPDATE            -- lock the appointment row
  re-check status and start time
  UPDATE original SET status='cancelled', cancellation_reason='rescheduled'
  INSERT replacement SET status='booked', rescheduled_from_id=<original>
COMMIT
```

The `FOR UPDATE` row lock is what stops two concurrent reschedules of the same appointment from
both cancelling it and inserting two replacements. If the replacement insert hits a taken slot,
the transaction rolls back and **the original appointment is still booked** — there is a test
for exactly that.

### 1.7 Time model

The clinic operates in one timezone, `Africa/Nairobi` by default, configurable with `CLINIC_TZ`.

Appointment times are stored as wall-clock values. To compare a stored slot against "now", the
service constructs the slot's absolute instant with `time.Date(..., clinicLoc)` and compares it
with the clock. This is exact because Nairobi has a fixed +03:00 offset and no daylight saving.
**The wall-clock model depends on that**, and the limitation is listed in §7.

The same predicate is used in two places, which is why the API never offers a slot it would
then refuse:

- `POST /appointments` rejects a slot that starts less than an hour from now.
- `GET /doctors/{id}/availability` removes slots that start less than an hour from now.

A slot starting *exactly* 60 minutes away is allowed in both.

---

## 2. Key Decisions & Trade-offs

### 2.1 PostgreSQL, not SQLite

The assessment allowed any database. SQLite would have made the deployment trivial: one file, one
container, no managed instance.

**PostgreSQL was chosen** because the requirements in this brief are concurrency requirements,
and PostgreSQL is where those requirements are actually testable. A partial unique index, a
`SELECT ... FOR UPDATE` row lock, a real transaction that rolls back, and two connections
genuinely competing are all things Postgres does and SQLite does not do in the same way.

The cost is real: the deployment needs a managed database, and CI needs a service container. The
benefit is that the headline claim — *a slot cannot be double-booked* — is enforced by the
database and demonstrated by a test with eight goroutines racing for one slot.

### 2.2 Wall-clock times, not `TIMESTAMPTZ`

Appointment times could have been stored as absolute instants in `TIMESTAMPTZ`.

**Wall-clock values were chosen** because the domain is fundamentally local. "The doctor works
09:00 to 17:00" and "the appointment is on the 22nd at 09:30" are statements about the clinic's
clock, not about instants. Storing them naively makes the slot grid arithmetic trivial, makes
the partial unique index obviously correct, and makes the API's `start_time: "09:30"` mean
exactly what it says.

The trade-off is that the model assumes a single, DST-free timezone. With a zone that observes
daylight saving, `time.Date` can normalise a non-existent local time, and the wall-clock
arithmetic stops being exact. The mitigation is that the clinic zone is configurable but
documented, and the IANA database is embedded in the binary so the deployment cannot drift from
the source.

### 2.3 A partial unique index, not application-level checks

The obvious implementation is "check whether the slot is free, then insert it". That has a race
window between the check and the insert, and the window is exactly where double-bookings come
from.

**A partial unique index was chosen** so the database is the arbiter. The application attempts
the insert optimistically and translates a `23505` violation into `409`. The `WHERE
status <> 'cancelled'` clause is what makes cancelling free the slot without a cleanup job.

A range exclusion constraint (`tstzrange` plus `btree_gist`) would also work, and would support
variable-length appointments. It was not used because the duration is fixed at 30 minutes and
starts are grid-aligned, which makes identical start times the only possible overlap — the
simple index is sufficient, and simpler indexes are easier to reason about and to migrate.

### 2.4 Standard library router, not chi or gin

Go 1.22's `net/http` `ServeMux` understands method-and-wildcard patterns natively:

```go
mux.HandleFunc("GET /doctors/{id}/availability", api.availability)
id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
```

**The standard library was chosen** because it removed a dependency rather than adding one.
There is no middleware library to learn and no dependency to audit. The middleware this service
needs — request logging and panic recovery — is about forty lines.

### 2.5 Cancel and re-insert for rescheduling, not in-place update

An in-place `UPDATE` of the date and time would keep the appointment id stable, which is
friendlier to a client holding an id.

**Cancel and re-insert was chosen** because it preserves the history: the original slot and the
reason it was freed are both still on record, and `rescheduled_from_id` links the replacement to
its predecessor. The cost is that the appointment id changes, and the patient sees two entries —
one cancelled, one booked — in their history. `GET /patients/{id}/appointments?include_cancelled=true`
exposes that trail deliberately.

### 2.6 A service layer, and rules that are pure functions

Business rules could have lived in the HTTP handlers, which is where a lot of Go web services
put them.

**A service layer was chosen** because the rules are the interesting part of this brief and
they should be testable without a database. Every rule is a pure function that takes values and
returns a typed error; the clock is injected, so the one-hour boundary can be asserted exactly
rather than approximately.

### 2.7 No authentication

The brief does not mention authentication and the API has none.

**This is a deliberate decision, not an oversight.** The scope of the brief is booking logic,
and adding auth would have meant inventing a user model, a token format and a session story
that the brief does not ask for — all of which would have gone untested against real
requirements. The clinic scenario implies a patient-facing front end that would carry identity
before it reaches this API.

The honest gap is that the endpoints are unauthenticated *and* un-rate-limited, so anyone who
can reach the deployment can book and cancel appointments. Both are listed in §7.

### 2.8 400 versus 422

The brief requires "meaningful error messages with correct HTTP status codes" without saying
what correct means.

**The split used here is 400 for unreadable requests and 422 for readable-but-disallowed ones.**
A malformed body or an unparseable date is `400`; a date that parses but falls outside the
doctor's hours is `422`. The alternative — everything is `400` — is simpler but collapses
"you sent nonsense" and "that slot is not available" into the same answer.

---

## 3. How to Run Locally

### 3.1 With Docker (recommended)

```bash
git clone https://github.com/PhilN8/clinic-booking-system.git
cd clinic-booking-system
docker compose up -d
```

That starts Postgres and the API. The API is on <http://localhost:8080>, Postgres on
`localhost:5432`. Migrations and the seed data are applied automatically on boot.

```bash
curl http://localhost:8080/healthz
# {"status":"ok"}
```

### 3.2 Without Docker

You need a PostgreSQL 18 instance.

```bash
git clone https://github.com/PhilN8/clinic-booking-system.git
cd clinic-booking-system

export DATABASE_URL="postgres://postgres:postgres@localhost:5432/clinic?sslmode=disable"
export CLINIC_TZ="Africa/Nairobi"

go run ./cmd/api
```

The server listens on `$PORT` (default `8080`).

### 3.3 A walkthrough

The seeded database contains five doctors and two patients with predictable ids. Set a date
tomorrow, because bookings in the past are refused:

```bash
DATE=$(date -d "tomorrow" +%F)        # GNU date
# DATE=$(date -v+1d +%F)               # macOS / BSD date
BASE=http://localhost:8080
```

**List the doctors**

```bash
curl -s $BASE/doctors
```

```json
[{"id":1,"name":"Dr. Amara Ochieng","start_working_hour":"09:00","end_working_hour":"17:00"},
 {"id":2,"name":"Dr. Brian Otieno","start_working_hour":"08:00","end_working_hour":"16:00"},
 {"id":3,"name":"Dr. Chloe Mwangi","start_working_hour":"10:00","end_working_hour":"18:00"},
 {"id":4,"name":"Dr. Daniel Kimani","start_working_hour":"09:00","end_working_hour":"13:00"},
 {"id":5,"name":"Dr. Esther Wanjiku","start_working_hour":"13:00","end_working_hour":"17:00"}]
```

**See the free slots**

```bash
curl -s "$BASE/doctors/1/availability?date=$DATE"
```

```json
{"doctor_id":1,"date":"2026-10-23","slots":[{"start":"09:00","end":"09:30"},{"start":"09:30","end":"10:00"}, ...]}
```

**Book one**

```bash
curl -s -X POST $BASE/appointments \
  -H "Content-Type: application/json" \
  -d "{\"doctor_id\":1,\"patient_id\":1,\"appointment_date\":\"$DATE\",\"start_time\":\"09:30\"}"
```

```json
{"id":1,"doctor_id":1,"patient_id":1,"appointment_date":"2026-10-23","start_time":"09:30",
 "end_time":"10:00","status":"booked","cancellation_reason":null,"cancelled_at":null,
 "created_at":"2026-10-21T11:02:01Z","rescheduled_from_id":null}
```

**Book it again — the database refuses**

```bash
curl -s -X POST $BASE/appointments \
  -H "Content-Type: application/json" \
  -d "{\"doctor_id\":1,\"patient_id\":1,\"appointment_date\":\"$DATE\",\"start_time\":\"09:30\"}"
```

```json
{"error":{"code":"SLOT_TAKEN","message":"slot 09:30 on 2026-10-23 is already booked for doctor 1",
 "details":{"appointment_date":"2026-10-23","doctor_id":1,"start_time":"09:30"}}}
```

with HTTP status `409`.

**Reschedule it**

```bash
curl -s -X PATCH $BASE/appointments/1/reschedule \
  -H "Content-Type: application/json" \
  -d "{\"new_appointment_date\":\"$DATE\",\"new_start_time\":\"14:00\"}"
```

```json
{"id":3,"doctor_id":1,"patient_id":1,"appointment_date":"2026-10-23","start_time":"14:00",
 "end_time":"14:30","status":"booked","cancellation_reason":null,"cancelled_at":null,
 "created_at":"2026-10-21T11:11:53Z","rescheduled_from_id":1}
```

The rescheduled appointment has a **new id** — see §2.5 — and `rescheduled_from_id` points back
at the original. (The id is `3` rather than `2` because the rejected duplicate booking above
still consumed a sequence value; that is Postgres, not the API.)

**Cancel it**

```bash
curl -s -X PATCH $BASE/appointments/3/cancel \
  -H "Content-Type: application/json" \
  -d '{"reason":"Patient no longer needs the appointment"}'
```

```json
{"id":3,"doctor_id":1,"patient_id":1,"appointment_date":"2026-10-23","start_time":"14:00",
 "end_time":"14:30","status":"cancelled",
 "cancellation_reason":"Patient no longer needs the appointment",
 "cancelled_at":"2026-10-21T11:11:53Z","created_at":"2026-10-21T11:11:53Z","rescheduled_from_id":1}
```

**See the patient's appointments**

```bash
curl -s "$BASE/patients/1/appointments?include_cancelled=true"
```

```json
{"patient_id":1,"appointments":[
  {"id":1,"...":"...","status":"cancelled","cancellation_reason":"rescheduled"},
  {"id":3,"...":"...","status":"cancelled","cancellation_reason":"Patient no longer needs the appointment"}]}
```

### 3.4 Configuration

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `DATABASE_URL` | **yes** | — | Postgres connection string |
| `PORT` | no | `8080` | HTTP listen port |
| `CLINIC_TZ` | no | `Africa/Nairobi` | IANA timezone the clinic operates in |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn` or `error` |
| `TEST_DATABASE_URL` | no | — | Used by the integration tests; they skip without it |

---

## 4. API Reference

Request and response bodies are JSON. Unknown request fields are rejected with `400` rather
than ignored.

### `POST /appointments` → `201 Created`

```json
{
  "doctor_id": 1,
  "patient_id": 1,
  "appointment_date": "2026-10-23",
  "start_time": "09:30"
}
```

`start_time` is a 24-hour time of day in `HH:MM` form. `appointment_date` is `YYYY-MM-DD`.

```json
{
  "id": 1,
  "doctor_id": 1,
  "patient_id": 1,
  "appointment_date": "2026-10-23",
  "start_time": "09:30",
  "end_time": "10:00",
  "status": "booked",
  "cancellation_reason": null,
  "cancelled_at": null,
  "created_at": "2026-10-21T11:02:01Z",
  "rescheduled_from_id": null
}
```

### `GET /doctors/{id}/availability?date=YYYY-MM-DD` → `200 OK`

```json
{
  "doctor_id": 1,
  "date": "2026-10-23",
  "slots": [
    { "start": "09:00", "end": "09:30" },
    { "start": "09:30", "end": "10:00" }
  ]
}
```

Slots that are already booked and slots that start within the next hour are not returned. A date
in the past returns an empty list with `200`, not an error.

### `PATCH /appointments/{id}/cancel` → `200 OK`

The body is optional. `reason` is stored on the appointment.

```json
{ "reason": "Patient no longer needs the appointment" }
```

Returns the updated appointment with `status: "cancelled"`, the `cancellation_reason` and a
`cancelled_at` timestamp.

### `PATCH /appointments/{id}/reschedule` → `200 OK`

```json
{
  "new_appointment_date": "2026-10-24",
  "new_start_time": "14:00"
}
```

Returns the **new** appointment. The original is cancelled with the reason `rescheduled`, and
`rescheduled_from_id` points back at it. The new appointment has a new id.

### `GET /patients/{id}/appointments` → `200 OK` *(bonus)*

Upcoming booked appointments for a patient, sorted by date and start time. Add
`?include_cancelled=true` to also include cancelled appointments, past and future — this is what
makes a reschedule's audit trail visible.

```json
{ "patient_id": 1, "appointments": [ { "id": 1, "...": "..." } ] }
```

### `GET /doctors`, `GET /doctors/{id}`, `GET /patients/{id}` → `200 OK`

Read-only listings, so the ids needed to call the API can be discovered without reading this
file.

### `GET /healthz` → `200 OK`

```json
{ "status": "ok" }
```

Pings the database. The deploy platform uses this as its health check, so a container that
cannot reach Postgres is reported unhealthy rather than silently serving errors.

### Errors

Every non-2xx response uses the same envelope:

```json
{
  "error": {
    "code": "OUTSIDE_WORKING_HOURS",
    "message": "start time 18:30 is outside the doctor's working hours (10:00-18:00)",
    "details": { "start_time": "18:30", "shift_start": "10:00", "shift_end": "18:00" }
  }
}
```

`details` is optional.

| Status | Meaning | Error codes |
|---|---|---|
| `400` | The request could not be read | `INVALID_JSON`, `INVALID_REQUEST`, `INVALID_DATE`, `INVALID_TIME` |
| `404` | The resource does not exist | `DOCTOR_NOT_FOUND`, `PATIENT_NOT_FOUND`, `APPOINTMENT_NOT_FOUND` |
| `409` | The resource is in a conflicting state | `SLOT_TAKEN`, `APPOINTMENT_ALREADY_CANCELLED` |
| `422` | The request was read but is not allowed | `OUTSIDE_WORKING_HOURS`, `SLOT_NOT_ALIGNED`, `TOO_SOON`, `APPOINTMENT_IN_PAST` |
| `500` | Something went wrong on the server | `INTERNAL_ERROR` |

---

## 5. Testing

```bash
go test ./...                       # unit tests; integration tests skip themselves
go test -race ./...                 # with the race detector
go vet ./...
```

The integration tests need a database. They skip themselves when `TEST_DATABASE_URL` is unset,
so `go test ./...` passes on a laptop with no Postgres:

```bash
export TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/clinic_test?sslmode=disable"
go test ./...
```

With Docker:

```bash
docker run -d --name clinic-test-pg -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_USER=postgres -e POSTGRES_DB=clinic_test -p 5432:5432 postgres:18-alpine
export TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/clinic_test?sslmode=disable"
go test ./...
```

### What is covered

**Unit tests (`internal/service`, `internal/handlers`)** — table-driven throughout:

| Area | Cases |
|---|---|
| Date parsing | valid dates, leap days, `2026-02-30`, wrong separators, unpadded values |
| Time parsing | `09:00` accepted, `9:00` rejected, `23:60` rejected, `24:00` rejected, seconds rejected |
| Slot grid | 16 slots for a full day, 1 slot for a minimal shift, contiguity, exact length |
| Grid alignment | on-grid and off-grid starts, times before the shift |
| Working hours | first slot, last slot, one minute early, one minute late, inclusive bounds |
| The one-hour rule | exactly 60 minutes **allowed**, 59m59s rejected, 30 minutes rejected, in the past rejected |
| Error mapping | every error code to its HTTP status |
| Handlers | status codes, response bodies, the error envelope, unknown fields, trailing JSON, bad path ids |

**Integration tests (`internal/service/integration_test.go`)** — against a real PostgreSQL:

| Case | What it proves |
|---|---|
| Booking the same slot twice | The second attempt returns `409` and exactly one row survives |
| Eight goroutines racing for one slot | Exactly one succeeds, seven get `409`, one row exists |
| Cancelling | The slot becomes bookable again |
| Cancelling twice | The second returns `409` |
| Rescheduling | The original is cancelled with reason `rescheduled`, the replacement is booked, `rescheduled_from_id` is set, the original slot is free again |
| Rescheduling onto an occupied slot | `409`, **and the original is still booked** — the transaction rolled back |
| Rescheduling onto the same slot | Allowed; the old row is cancelled and the new one booked |
| Migrations run twice | Idempotent |
| Wall-clock round trip | A booked 13:30 slot reads back as 13:30 with the right end time and date |

---

## 6. Deployment

### 6.1 What is deployed, and where

The service is deployed to **Render** as a Docker web service with a managed PostgreSQL
instance, from `render.yaml` in this repository.

| | |
|---|---|
| **Live site** | <https://clinic-booking-system-1-yu19.onrender.com/doctors> |
| **Live API** | <https://clinic-booking-system-1-yu19.onrender.com/api/doctors> |
| **Health check** | <https://clinic-booking-system-1-yu19.onrender.com/healthz> |
| **Deploy trigger** | a push to `main` |
| **Database** | Render managed PostgreSQL 18, free plan |

### 6.1a One server, two faces

The same binary serves both the site and the JSON API, on one port. Three paths exist as both
an endpoint and a page:

| Path | Browser (`Accept: text/html`) | API client |
|---|---|---|
| `GET /doctors` | the doctor list | the doctor list as JSON |
| `GET /doctors/{id}` | the availability and booking page | the doctor as JSON |
| `GET /patients/{id}/appointments` | the appointment list | the appointments as JSON |

They negotiate on the `Accept` header. A browser sends `text/html` and gets the page; `curl`
sends `*/*` and gets JSON, so the examples in §3.3 and §4 work unchanged. The whole JSON API is
also mounted under **`/api`**, where it is always JSON and never negotiates.

### 6.2 The pipeline

`.github/workflows/ci.yml` runs two jobs.

**`test`** — on every pull request and every push to `main`:

1. `go mod verify`
2. `go vet ./...`
3. `go test -race -coverprofile=coverage.out ./...`, with a `postgres:18` service container so
   the integration tests actually run rather than skipping
4. `go build ./...`

**`publish`** — only on a push to `main`, and only if `test` passed: build the Docker image and
push it to Docker Hub, tagged with the commit SHA and `latest`, using the GitHub Actions build
cache. It is skipped with a notice if the Docker Hub credentials are not configured.

Pull requests run the checks and never touch the live environment.

### 6.3 How the deploy is triggered

There is exactly **one** deploy path, and it runs through the pipeline:

```
PR opened ────► Actions: test ────► must pass to merge
                      │
merge to main ───────┴──► Actions: test ──► (passes)
                                             │
                                             ▼
                          Render: autoDeployTrigger: checksPass
                          Render sees the checks passed on main
                          and rebuilds the Dockerfile and redeploys
```

`autoDeployTrigger: checksPass` in `render.yaml` is what closes the loop. Render watches the
linked branch and waits for its CI checks to pass before it deploys, so a red test suite on
`main` blocks the release rather than shipping it. The pipeline genuinely gates the deploy
instead of merely running alongside it.

This is also why the CI workflow does **not** call Render's deploy hook. Render's own
`checksPass` trigger already fires once the checks pass, and calling the hook as well would
deploy twice.

**Optional repository secrets.** Every job that needs a secret degrades gracefully without it, so
a misconfigured pipeline never turns `main` red:

| Secret | Purpose | If unset |
|---|---|---|
| `DOCKERHUB_USERNAME` | Docker Hub account to publish the image to | The image push is skipped with a notice |
| `DOCKERHUB_TOKEN` | Docker Hub access token | As above |
| `RENDER_API_KEY` | Render API key, to validate the blueprint | The blueprint validation is skipped with a notice |
| `RENDER_WORKSPACE_ID` | Render workspace ID, to validate the blueprint | As above |

Render's deploy hook is still available in the dashboard for a manual redeploy, but nothing in
the pipeline needs it.

### 6.4 Validating the blueprint

`render.yaml` is validated in CI by Render's own
[Validate Blueprint endpoint](https://api-docs.render.com/reference/validate-blueprint), which
checks the syntax and structure and reports the resources the Blueprint would create. That turns
a broken Blueprint into a failed pull request check rather than a failed deployment.

The endpoint needs a Render API key and the workspace ID, so the step is skipped with a notice
until both are set as repository secrets. To set them: **Render → Workspace Settings → API
Keys** for the key, and **Workspace Settings → General** for the workspace ID.

The Blueprint can also be validated locally against Render's published JSON Schema, which is
what the IDE's YAML extension uses:

```bash
curl -fsS https://render.com/schema/render.yaml.json -o schema.json
# then point your YAML extension at it, or use a JSON Schema validator
```

### 6.5 Launching it on Render

There are two ways in. The blueprint is the one to prefer because it creates the database and
wires `DATABASE_URL` for you.

**Option A — Blueprint (recommended)**

```bash
# 1. In Render: New -> Blueprint, point it at the repository, and apply.
#    render.yaml creates both the web service and the PostgreSQL instance,
#    sets runtime: docker and autoDeployTrigger: checksPass, and wires
#    DATABASE_URL.
```

**Option B — the service already exists**

If the service was created from the dashboard, check these settings under **Settings -> Build &
Deploy**:

1. **Runtime must be `Docker`.** Render also offers a native Go runtime, which ignores the
   `Dockerfile` and runs `go build -tags netgo -ldflags '-s -w' -o app` in the repository root.
   That fails here, because the main package lives in `./cmd/api`, not at the root.
2. **Auto-Deploy must be `checksPass`**, with the branch set to `main`. `checksPass` is what
   makes Render wait for the CI checks before redeploying.

If you would rather keep the native Go runtime, use these build settings instead of switching:

| Setting | Value |
|---|---|
| Build Command | `go build -tags netgo -ldflags '-s -w' -o app ./cmd/api` |
| Start Command | `./app` |

Either way, make sure the service has `DATABASE_URL` pointing at a Postgres instance and
`CLINIC_TZ` set to `Africa/Nairobi`. Render injects `PORT` itself.

> **Keeping the service name matters.** The Blueprint adopts an existing service when its `name`
> matches exactly. `clinic-booking-system-1` is the name Render generated; if the Blueprint
> names it something else, Render creates a *second* service and leaves the original running.

### 6.6 Upgrading an existing Render database

`postgresMajorVersion` in `render.yaml` applies when the database is **created**. It does not
migrate a database that already exists, so an instance created against the previous
`postgresMajorVersion` stays on the old major version.

Render offers two routes, from its own documentation:

- **In-place upgrade** — always moves to the latest major version Render supports, which is
  currently 18. It requires downtime.
- **Create a new database on 18 and point the service at it** — this is the option to take here.
  The database holds only seed data and bookings created while testing, so there is nothing worth
  migrating: create a fresh PostgreSQL 18 instance, copy its **External Database URL** into the
  web service's `DATABASE_URL`, and redeploy. The migrations and the seed run automatically on
  boot.

### 6.7 Deployment status

The service is **live** at <https://clinic-booking-system-1-yu19.onrender.com>. It runs on
Render's Docker runtime, builds from the `Dockerfile`, and redeploys automatically on a push to
`main` once the CI checks pass.

### 6.8 Troubleshooting

**`go: go.mod file not found in current directory or any parent directory`**

Render deployed a commit that predates the module — check that the branch it deploys from
actually contains `go.mod`. This is what happens when a pull request has not been merged into
`main` yet.

**`Using Go version ...` followed by `go build -tags netgo`**

The service is using Render's native Go runtime instead of Docker, so the `Dockerfile` is being
ignored. See 6.3.

**`no Go files in ...` or `main.go: no such file or directory`**

Same cause as above: the native runtime builds from the repository root, but the main package is
at `./cmd/api`. Either switch to the Docker runtime or set the build command from 6.3.

**`failed to connect to ... database`**

The `DATABASE_URL` is wrong or the database is not in the same region as the service. Check the
Render dashboard for the database's connection string and copy it into the service's environment
variables.

**`The suggested container configuration for 18+ is to place a single mount at
/var/lib/postgresql`**

PostgreSQL 18's Docker image moved its data directory to `/var/lib/postgresql/18/docker` and
declares `/var/lib/postgresql` as the volume. A volume mounted at `/var/lib/postgresql/data` —
the PostgreSQL 16 layout — is rejected outright by the image. `docker-compose.yml` mounts the
parent directory for this reason.

**`database "clinic_test" does not exist`**

The integration tests need their own database. The compose stack creates `clinic`; create the
test one alongside it:

```bash
docker compose exec postgres psql -U postgres -c "CREATE DATABASE clinic_test;"
```

---

## 7. Known Limitations & Future Work

**Deliberate simplifications**

- **Doctors work all seven days** with a single daily window. A real clinic needs per-weekday
  hours and days off. The natural extension is a `working_days` column on `doctors` and a
  weekday mask in the slot generator; the schema constraint that working hours fall on a
  half-hour boundary would stay as it is.
- **No authentication or authorisation.** Anyone who can reach the deployment can book and
  cancel. Adding it means a patient identity in the request, and the `doctor` and `patient`
  ids would need to be derived from the authenticated principal rather than taken from the body.
- **No rate limiting.** The API is intended to sit behind a front end that rate-limits per
  patient.

**Gaps I would close next**

- **No pagination.** `GET /patients/{id}/appointments` returns every match. It needs a cursor
  once a patient has more than a page of history.
- **No write endpoints for doctors or patients.** The seed migration supplies them. Creating and
  editing them is admin functionality that the brief did not ask for.
- **Appointments are never "completed".** A `booked` appointment whose start time has passed is
  still `booked`, and still occupies its slot, which is correct — but a `completed` status would
  let the clinic report on throughput.
- **One clinic, one timezone.** The wall-clock model and the fixed +03:00 offset assumption are
  fine for one clinic and would need revisiting for a second clinic in a different zone, or for
  a zone with daylight saving.
- **The unique index protects start times, not ranges.** That is sufficient only while the
  duration is fixed at 30 minutes. Variable-length appointments would need a `tstzrange`
  exclusion constraint with `btree_gist`.
- **No structured request ids.** The logs carry method, path, status and duration, which is
  enough to debug, but correlating a request across the service and the database would be
  easier with a request id in the log line and the response header.

---

## 8. AI Reflection

### 8.1 What I used AI for

Across the four sections:

- **Section 1 (design).** I had the AI interrogate the brief before writing any code. It
  produced a list of ambiguities in the requirements — the missing timezone, the unstated
  appointment duration, the missing service layer, the reschedule race — which I then resolved
  by making explicit decisions and recording them in `AGENTS.md` and in §2 of this README.
- **Section 2 (implementation).** Mostly me writing Go, with AI assistance for the mechanical
  parts: the pgx error inspection for unique violations, the `httptest` scaffolding, and the
  SQL for the slot queries.
- **Section 3 (deployment).** The AI drafted the `Dockerfile`, the `render.yaml` blueprint and
  the GitHub Actions workflow. I wrote the deployment steps by hand against Render's and
  Docker Hub's actual interfaces.
- **Section 4 (this section).** I asked the AI to interrogate the brief specifically for holes
  and contradictions, and it found several that I would not have found on my own — see 8.3.

### 8.2 An example where the AI improved my work

**Prompt:** *"Review this clinic booking API brief for holes, ambiguities and contradictions.
Focus on the schema, the concurrency guarantees and the test requirements."*

The most valuable thing that came back was this observation: **the prescribed unique index does
not actually prevent double-booking.**

The brief asked for a composite unique index on `(doctor_id, appointment_date, start_time)`. That
index prevents two appointments starting at the *same* instant. It says nothing about two
appointments that *overlap* — a 09:15 appointment inserted alongside a 09:00 appointment
produces no index violation at all, because 09:15 is a distinct key. Both bookings go through
and the doctor is double-booked.

The follow-on question — "when does the index actually suffice?" — has a clean answer: when
every appointment is exactly 30 minutes long and every start lands on the 30-minute grid. Under
those two rules, overlap implies an identical start time, and the index catches it. So the fix
was not a more exotic constraint; it was to **make the duration fixed and the grid mandatory**,
which the brief had left as an unstated assumption.

That single observation changed the schema (a `CHECK` constraint pinning the duration), the
service layer (grid alignment became a validation rule rather than a convenience), and the test
suite (it became a testable property rather than an assumption). I would not have noticed the
gap on my own from reading the brief, because the index in the brief looks like it does the job.

### 8.3 Examples where the AI output was wrong, and how I caught it

**1. A test that asserted the opposite of the truth.** I wrote a table-driven test for the slot
grid with a case named "after the shift" expecting `IsOnGrid(09:00, 17:00)` to be `false`. The
grid is anchored at the start of the shift and knows nothing about where the shift ends — 17:00
*is* on the grid, and it is the working-hours check that rejects it. The test failed, and the
fix was to correct the test's expectation and rename the case to say what it actually means. I
had written the test from my mental model of the function rather than from the function itself.

**2. A parser that was not as strict as the specification required.** The brief says a time of
day is `HH:MM`. I wrote `time.Parse("15:04", value)` and assumed that enforced two-digit hours.
It does not: Go's `15` layout accepts a single-digit hour, so `"9:00"` parsed successfully as
09:00. The unit test for it failed, and the fix was to format the parsed value back and compare
it with the input, so `"9:00"` is rejected and `"09:00"` is accepted. The bug was in the
implementation, and the specification was right.

**3. An assumption about the driver that was simply false.** I wrote the repository expecting
`pgx` to return `TIME` and `DATE` columns as `time.Time`, the way most ORMs do. The first
integration test against real PostgreSQL failed with `unsupported Scan, storing driver.Value type
string into type *time.Time` — through `database/sql`, `pgx` hands `TIME` back as the text
`"09:00:00"`. The fix was to scan into `any` and convert explicitly, accepting either
representation. Nothing about the unit tests could have caught this; it only appeared once the
code ran against a real database, which is exactly why the integration tests were worth writing.

**4. A test assertion that was written backwards.** The integration test for rescheduling
asserted that the original slot was *absent* from the availability list after a reschedule. The
opposite is true — a reschedule frees the original slot, so it must be *present*. The test failed
against working code, and the fix was to invert the assertion. The code was right; my reading
of the requirement was wrong.

**5. A requirement ordering that produced a worse error message.** The brief's validation order
checked grid alignment before working hours, which meant a request for 08:30 when the doctor
starts at 09:00 was reported as "not on the 30-minute grid" — technically true, but it points
at the wrong problem. I changed the order to check the working window first and recorded the
change in `AGENTS.md` rather than deviating from it silently.

### 8.4 Two decisions I made without the AI

**1. The placement of the "this appointment has already started" rule.** The brief puts it
inside the transaction, in the repository. I decided the rule belongs in the service layer,
which owns the business rules, with the repository re-checking it under the row lock as a
safety net rather than as the primary implementation. The reason is error quality: the service
can read the appointment and report *"appointment 3 has already started and cannot be
cancelled"*, whereas a repository that can only return a sentinel error forces the service into
a generic message. The repository still enforces it, so correctness does not depend on where the
check lives — only the quality of the response does.

**2. Treating "already cancelled" as `409 Conflict` rather than `422 Unprocessable
Entity`.** The request is well-formed and it is the *resource's state* that makes it
impossible, which is what `409` is for. `422` is reserved for requests that are well-formed but
inherently disallowed, such as a slot outside working hours. This is a judgement about REST
semantics rather than a technical fact, and I would rather defend it than have it made by
default.

---

## 9. Design System

The patient-facing site has a documented design system in [`design.md`](design.md). It covers the
palette, typography, spacing, component inventory, the booking flow, content tone and the
WCAG 2.2 AA checklist, and it maps each component back to the endpoint it is built from.

The site is **rendered on the server** with Go templates, so it works without JavaScript and the
JSON API contract is untouched. The stylesheet contains no hex value: the palette is generated
from `design/tokens.json` into CSS custom properties at request time, so it cannot drift from
the tokens.

The colour decisions are **enforced by the test suite** rather than asserted in prose.
`design/tokens.json` is the source of truth, and `design/tokens_test.go` fails `go test ./...` if
any documented pairing drops below its contrast requirement:

```bash
go test ./design/ -v     # verifies every pairing and prints the table
```

The test also catches a malformed hex value, two token names sharing a colour, and a scale whose
shades run the wrong way round. The contrast table in `design.md` is that test's output and is
verified to match it.

Two decisions in there are worth calling out here because they constrain the API:

- **Available slots are neutral, not green.** A free slot is the default state, so it carries no
  colour; the only colour in the slot grid is the selection. Green is reserved for a completed
  action and red for cancellation or error, which keeps green and red from being the one pair a
  colour-blind reader has to separate.
- **The interface has to explain the one-hour rule.** The grid shows every slot with its status —
  available, taken, too soon, or past — and states the rule plainly above it, rather than leaving
  the patient to think it is a bug. The public availability endpoint still returns only the free
  slots; the richer grid comes from an internal service method that applies the same rules.
