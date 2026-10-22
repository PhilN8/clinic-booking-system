# Clinic booking system: reference

The companion reference for the [README](README.md). The README answers sections 1, 3 and 4 of
the assessment. This file holds what a reviewer needs to run the code and call the API.

| | |
|---|---|
| **Live site** | <https://clinic-booking-system-1-yu19.onrender.com> |
| **Live API** | <https://clinic-booking-system-1-yu19.onrender.com/api/doctors> |
| **API spec** | <https://clinic-booking-system-1-yu19.onrender.com/openapi.json> |
| **API reference** | <https://clinic-booking-system-1-yu19.onrender.com/docs> |

## Contents

- [Running it locally](#running-it-locally)
- [API reference](#api-reference)
- [Testing](#testing)
- [Deployment reference](#deployment-reference)
- [Known limitations and future work](#known-limitations-and-future-work)
- [Design system](#design-system)

## Running it locally
### With Docker (recommended)

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

### Without Docker

You need a PostgreSQL 18 instance.

```bash
git clone https://github.com/PhilN8/clinic-booking-system.git
cd clinic-booking-system

export DATABASE_URL="postgres://postgres:postgres@localhost:5432/clinic?sslmode=disable"
export CLINIC_TZ="Africa/Nairobi"

go run ./cmd/api
```

The server listens on `$PORT` (default `8080`).

### A walkthrough

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

**Book it again, and the database refuses**

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

The rescheduled appointment has a **new id**, and `rescheduled_from_id` points back
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

### Configuration

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `DATABASE_URL` | **yes** | none | Postgres connection string |
| `PORT` | no | `8080` | HTTP listen port |
| `CLINIC_TZ` | no | `Africa/Nairobi` | IANA timezone the clinic operates in |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn` or `error` |
| `TEST_DATABASE_URL` | no | none | Used by the integration tests; they skip without it |

## API reference

Every endpoint is described by an OpenAPI 3.1 document, served as JSON at `/openapi.json` and
rendered with a browsable reference at `/docs`. The document carries the domain rules, the
request and response examples, and the error vocabulary.

It is kept honest by a test rather than by discipline. The route table in
`internal/server/routes.go` drives the router, and the same table is compared against the
document: a route that is not documented, a documented path that nothing serves, a duplicated
`operationId`, an error code missing from the vocabulary and a `$ref` that resolves to nothing
all fail the build.

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
`?include_cancelled=true` to also include cancelled appointments, past and future, which is what
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

## Testing
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

**Unit tests (`internal/service`, `internal/handlers`)**, table-driven throughout:

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

**Integration tests (`internal/service/integration_test.go`)**, against a real PostgreSQL:

| Case | What it proves |
|---|---|
| Booking the same slot twice | The second attempt returns `409` and exactly one row survives |
| Eight goroutines racing for one slot | Exactly one succeeds, seven get `409`, one row exists |
| Cancelling | The slot becomes bookable again |
| Cancelling twice | The second returns `409` |
| Rescheduling | The original is cancelled with reason `rescheduled`, the replacement is booked, `rescheduled_from_id` is set, the original slot is free again |
| Rescheduling onto an occupied slot | `409`, and **the original is still booked** because the transaction rolled back |
| Rescheduling onto the same slot | Allowed; the old row is cancelled and the new one booked |
| Migrations run twice | Idempotent |
| Wall-clock round trip | A booked 13:30 slot reads back as 13:30 with the right end time and date |

### Mistakes the tests caught

Most of the code was written by the AI, so most of the mistakes in it were the AI's. These two
were caught by the unit tests. The third, the `pgx` one, is in the README's AI reflection because
it only surfaced against a real database.

**A test that asserted the opposite of the truth.** A table-driven case named "after the shift"
expected `IsOnGrid(09:00, 17:00)` to be `false`. The grid is anchored at the start of the shift
and knows nothing about where the shift ends, so 17:00 is on the grid and the working-hours check
is what rejects it. The test failed, and the fix was to correct the expectation and rename the
case to say what it actually means.

**A parser that was not as strict as the specification required.** The brief says a time of day is
`HH:MM`. The code used `time.Parse("15:04", value)` and assumed that enforced two-digit hours.
Go's `15` layout accepts a single-digit hour, so `"9:00"` parsed as 09:00. The unit test failed,
and the fix was to format the parsed value back and compare it with the input.


## Deployment reference
### Validating the blueprint

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

### Launching it on Render

There are two ways in. The blueprint is the one to prefer because it creates the database and
wires `DATABASE_URL` for you.

**Option A: blueprint (recommended)**

```bash
# 1. In Render: New -> Blueprint, point it at the repository, and apply.
#    render.yaml creates both the web service and the PostgreSQL instance,
#    sets runtime: docker and autoDeployTrigger: checksPass, and wires
#    DATABASE_URL.
```

**Option B: the service already exists**

If the service was created from the dashboard, check these settings under **Settings, Build &
Deploy**.

The runtime must be `Docker`. Render also has a native Go runtime, which ignores the
`Dockerfile` and runs `go build -tags netgo -ldflags '-s -w' -o app` in the repository root.
That fails here, because the main package lives in `./cmd/api`, not at the root. Auto-Deploy
must be `checksPass`, with the branch set to `main`; `checksPass` is what makes Render wait for
the CI checks before redeploying.

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

### Upgrading an existing Render database

`postgresMajorVersion` in `render.yaml` applies when the database is **created**. It does not
migrate a database that already exists, so an instance created against the previous
`postgresMajorVersion` stays on the old major version.

Render documents two routes:

- In-place upgrade always moves to the latest major version Render supports, which is
  currently 18. It requires downtime.
- Create a new database on 18 and point the service at it. This is the option to take here.
  The database holds only seed data and bookings created while testing, so there is nothing worth
  migrating: create a fresh PostgreSQL 18 instance, copy its **External Database URL** into the
  web service's `DATABASE_URL`, and redeploy. The migrations and the seed run automatically on
  boot.

### Troubleshooting

**`go: go.mod file not found in current directory or any parent directory`**

Render deployed a commit that predates the module. Check that the branch it deploys from
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
declares `/var/lib/postgresql` as the volume. A volume mounted at `/var/lib/postgresql/data`,
the PostgreSQL 16 layout, is rejected outright by the image. `docker-compose.yml` mounts the
parent directory for this reason.

**`database "clinic_test" does not exist`**

The integration tests need their own database. The compose stack creates `clinic`; create the
test one alongside it:

```bash
docker compose exec postgres psql -U postgres -c "CREATE DATABASE clinic_test;"
```

## Known limitations and future work
**Deliberate simplifications**

- Doctors work all seven days with a single daily window. A real clinic needs per-weekday
  hours and days off. The natural extension is a `working_days` column on `doctors` and a
  weekday mask in the slot generator; the schema constraint that working hours fall on a
  half-hour boundary would stay as it is.
- No authentication or authorisation. Anyone who can reach the deployment can book and
  cancel. Adding it means a patient identity in the request, and the `doctor` and `patient`
  ids would need to be derived from the authenticated principal rather than taken from the body.
- No rate limiting. The API is intended to sit behind a front end that rate-limits per
  patient.

**Gaps I would close next**

- No pagination. `GET /patients/{id}/appointments` returns every match. It needs a cursor
  once a patient has more than a page of history.
- No write endpoints for doctors or patients. The seed migration supplies them. Creating and
  editing them is admin functionality that the brief did not ask for.
- Appointments are never "completed". A `booked` appointment whose start time has passed is
  still `booked`, and still occupies its slot, which is correct. A `completed` status would
  let the clinic report on throughput.
- One clinic, one timezone. The wall-clock model and the fixed +03:00 offset assumption are
  fine for one clinic and would need revisiting for a second clinic in a different zone, or for
  a zone with daylight saving.
- The unique index protects start times, not ranges. That is sufficient only while the
  duration is fixed at 30 minutes. Variable-length appointments would need a `tstzrange`
  exclusion constraint with `btree_gist`.
- No structured request ids. The logs carry method, path, status and duration, which is
  enough to debug, but correlating a request across the service and the database would be
  easier with a request id in the log line and the response header.

## Design system
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

- Available slots are neutral, not green. A free slot is the default state, so it carries no
  colour; the only colour in the slot grid is the selection. Green is reserved for a completed
  action and red for cancellation or error, which keeps green and red from being the one pair a
  colour-blind reader has to separate.
- The interface has to explain the one-hour rule. The grid shows every slot with its status
  (available, taken, too soon, or past) and states the rule plainly above it, rather than leaving
  the patient to think it is a bug. The public availability endpoint still returns only the free
  slots; the richer grid comes from an internal service method that applies the same rules.