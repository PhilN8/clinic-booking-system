# Clinic Booking System

A REST API for a small clinic with five doctors. Patients see which 30-minute slots are free for
a doctor on a given day, book one, cancel it, or move it to a different slot. A booked slot can
never be double-booked.

Built in Go with PostgreSQL. Standard library only, plus the Postgres driver.

The same binary also serves the patient-facing site, rendered on the server and styled from the
design tokens.

| | |
|---|---|
| **Live site** | <https://clinic-booking-system-1-yu19.onrender.com/doctors> |
| **Live API** | <https://clinic-booking-system-1-yu19.onrender.com/api/doctors> |

Sections 1, 3 and 4 of the assessment are answered here. The API reference, the local setup, the
test matrix, the known limitations and the design system are in
[`reference.md`](reference.md).

## Section 1: system design

### The scenario

Five doctors. Each has one working window a day, for example 09:00 to 17:00, divided into
sixteen 30-minute slots. A patient picks a doctor and a date, then books one of the free slots.
Bookings cannot overlap, and cancelling frees the slot again.

That reduces to three rules that the whole system is built around:

1. A slot is valid if it falls inside the doctor's working window, lands on the 30-minute
   grid, and is at least an hour away from now.
2. A slot is taken if a `booked` appointment exists for that doctor, date and start time.
3. Cancelling and rescheduling release the slot they occupied.

### Entities

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

`appointment_date`, `start_time` and `end_time` are naive wall-clock values in the clinic's
timezone. `created_at` and `cancelled_at` are absolute instants (`TIMESTAMPTZ`). The distinction
is deliberate: "the doctor works 09:00 to 17:00" is a local concept, while "when was this row
cancelled" is an event on a timeline.

### Components and layering

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
structurally, so the rules never depend on the database. That is what lets the rule tests
run without a database at all.

The rules live in the service layer because those rules are the product. Validation functions
take plain values and return typed errors:

```go
func ValidateSlot(doctor *models.Doctor, date, start time.Time, now time.Time, loc *time.Location) error
```

`now` is passed in instead of being read from the clock inside, so a test can pin it to an exact
instant and assert on exact boundaries.

### The slot model

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

Two properties fall out of this, and both matter. Working-hours bounds are inclusive: a 09:00
start is valid, a 16:30 start is valid because it ends exactly at 17:00, and a 17:00 start is
not. Grid alignment is mandatory: a 09:15 start is rejected, and that is what makes the
database constraint below sufficient to prevent double-booking. With a fixed 30-minute duration
and aligned starts, two appointments can only overlap if they start at the *same* instant.

### Preventing double-booking

The protection is a partial unique index:

```sql
CREATE UNIQUE INDEX appointments_no_double_booking
    ON appointments (doctor_id, appointment_date, start_time)
    WHERE status <> 'cancelled';
```

Three things follow from it:

- Two clients racing for one slot cannot both win. The second `INSERT` violates the index and
  is reported as `409 SLOT_TAKEN`.
- Cancelling frees the slot with no cleanup job: the cancelled row leaves the index because
  the index is partial.
- The application never needs a read-then-write check, which is where the classic race lives.
  The service attempts the insert and lets the database arbitrate.

So the API inserts and lets the unique index decide. It never checks availability first.

### Rescheduling atomically

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
the transaction rolls back and the original appointment is still booked. There is a test for
exactly that.

### Time model

The clinic operates in one timezone, `Africa/Nairobi` by default, configurable with `CLINIC_TZ`.

Appointment times are stored as wall-clock values. To compare a stored slot against "now", the
service constructs the slot's absolute instant with `time.Date(..., clinicLoc)` and compares it
with the clock. This is exact because Nairobi has a fixed +03:00 offset and no daylight saving.
The wall-clock model depends on that, and the limitation is listed in
[`reference.md`](reference.md).

The same predicate is used in two places, which is why the API never offers a slot it would
then refuse:

- `POST /appointments` rejects a slot that starts less than an hour from now.
- `GET /doctors/{id}/availability` removes slots that start less than an hour from now.

A slot starting *exactly* 60 minutes away is allowed in both.

### Decisions and trade-offs

#### PostgreSQL, not SQLite

The assessment allowed any database. SQLite would have made the deployment trivial: a single
file and no managed instance.

PostgreSQL was chosen because the requirements in this brief are concurrency requirements,
and PostgreSQL is where those requirements are actually testable. A partial unique index, a
`SELECT ... FOR UPDATE` row lock, a real transaction that rolls back, and two connections
genuinely competing are all things Postgres does and SQLite does not do in the same way.

The cost is real: the deployment needs a managed database, and CI needs a service container. The
benefit is that the central claim, that a slot cannot be double-booked, is enforced by the
database and demonstrated by a test with eight goroutines racing for one slot.

#### Wall-clock times, not `TIMESTAMPTZ`

Appointment times could have been stored as absolute instants in `TIMESTAMPTZ`.

Wall-clock values were chosen because the domain is local. "The doctor works 09:00 to 17:00" and
"the appointment is on the 22nd at 09:30" are statements about the clinic's clock, not about
instants. Storing them naively makes the slot grid arithmetic trivial, makes the partial unique
index obviously correct, and makes the API's `start_time: "09:30"` mean exactly what it says.

The trade-off is that the model assumes a single, DST-free timezone. With a zone that observes
daylight saving, `time.Date` can normalise a non-existent local time, and the wall-clock
arithmetic stops being exact. The mitigation is that the clinic zone is configurable but
documented, and the IANA database is embedded in the binary so the deployment cannot drift from
the source.

#### A partial unique index, not application-level checks

The obvious implementation is "check whether the slot is free, then insert it". That has a race
window between the check and the insert, and the window is where double-bookings come from.

A partial unique index was chosen so the database is the arbiter. The application attempts
the insert optimistically and translates a `23505` violation into `409`. The `WHERE
status <> 'cancelled'` clause is what makes cancelling free the slot without a cleanup job.

A range exclusion constraint (`tstzrange` plus `btree_gist`) would also work, and would support
variable-length appointments. It was not used because the duration is fixed at 30 minutes and
starts are grid-aligned, which makes identical start times the only possible overlap. The
simple index is sufficient, and simpler indexes are easier to reason about and to migrate.

#### The standard library router

Go 1.22's `net/http` `ServeMux` understands method-and-wildcard patterns natively:

```go
mux.HandleFunc("GET /doctors/{id}/availability", api.availability)
id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
```

The standard library was chosen because it removed a dependency. There is no middleware library
to learn and no dependency to audit. The middleware this service needs, request logging and panic
recovery, is about forty lines.

#### Cancel and re-insert for rescheduling

An in-place `UPDATE` of the date and time would keep the appointment id stable, which is
friendlier to a client holding an id.

Cancel and re-insert was chosen because it preserves the history: the original slot and the
reason it was freed are both still on record, and `rescheduled_from_id` links the replacement to
its predecessor. The cost is that the appointment id changes, and the patient sees two entries,
one cancelled and one booked, in their history.
`GET /patients/{id}/appointments?include_cancelled=true` exposes that trail deliberately.

#### A service layer, with rules as pure functions

Business rules could have lived in the HTTP handlers, which is where a lot of Go web services
put them.

A service layer was chosen because the rules are the interesting part of this brief and
they should be testable without a database. Every rule is a pure function that takes values and
returns a typed error; the clock is injected, so the one-hour boundary can be asserted exactly
rather than approximately.

#### No authentication

The brief does not mention authentication and the API has none. This is a deliberate decision.
The scope of the brief is booking logic, and adding auth would have meant inventing a user model
and a session story that the brief does not ask for, neither of which would have been tested
against real requirements. The clinic scenario implies a patient-facing front end that would
carry identity before it reaches this API.

The honest gap is that the endpoints are unauthenticated and un-rate-limited, so anyone who
can reach the deployment can book and cancel appointments. Both are listed in
[`reference.md`](reference.md).

#### 400 for unreadable requests, 422 for disallowed ones

The brief requires "meaningful error messages with correct HTTP status codes" without saying
what correct means.

The split used here is 400 for unreadable requests and 422 for readable-but-disallowed ones.
A malformed body or an unparseable date is `400`; a date that parses but falls outside the
doctor's hours is `422`. The alternative, everything is `400`, is simpler but collapses
"you sent nonsense" and "that slot is not available" into the same answer.

## Section 3: deployment and CI/CD

### What is deployed

Render runs the service as a Docker web service with a managed PostgreSQL instance, from
`render.yaml` in this repository.

| | |
|---|---|
| **Live site** | <https://clinic-booking-system-1-yu19.onrender.com/doctors> |
| **Live API** | <https://clinic-booking-system-1-yu19.onrender.com/api/doctors> |
| **Health check** | <https://clinic-booking-system-1-yu19.onrender.com/healthz> |
| **Deploy trigger** | a push to `main` |
| **Database** | Render managed PostgreSQL 18, free plan |

One binary serves both the site and the JSON API, on one port. Three paths exist as both an
endpoint and a page:

| Path | Browser (`Accept: text/html`) | API client |
|---|---|---|
| `GET /doctors` | the doctor list | the doctor list as JSON |
| `GET /doctors/{id}` | the availability and booking page | the doctor as JSON |
| `GET /patients/{id}/appointments` | the appointment list | the appointments as JSON |

They negotiate on the `Accept` header. A browser sends `text/html` and gets the page; `curl`
sends `*/*` and gets JSON, so the examples in [`reference.md`](reference.md) work unchanged. The
whole JSON API is also mounted under `/api`, where it is always JSON and never negotiates.

### The pipeline

`.github/workflows/ci.yml` runs two jobs.

`test` runs on every pull request and every push to `main`:

1. `go mod verify`
2. `go vet ./...`
3. `go test -race -coverprofile=coverage.out ./...`, with a `postgres:18` service container so
   the integration tests actually run rather than skipping
4. `go build ./...`

`publish` runs only on a push to `main`, and only if `test` passed. It builds the Docker image
and pushes it to Docker Hub, tagged with the commit SHA and `latest`, using the GitHub Actions
build cache. The job skips itself with a notice when the Docker Hub credentials are missing.

Pull requests run the checks and never touch the live environment.

### How a deploy is triggered

One deploy path runs through the pipeline:

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
`main` blocks the release. The pipeline gates the deploy rather than running alongside it.

The CI workflow therefore does not call Render's deploy hook. Render's own `checksPass` trigger
already fires once the checks pass, and calling the hook as well would deploy twice.

Every job that needs a secret degrades gracefully without it, so a misconfigured pipeline never
turns `main` red:

| Secret | Purpose | If unset |
|---|---|---|
| `DOCKERHUB_USERNAME` | Docker Hub account to publish the image to | The image push is skipped with a notice |
| `DOCKERHUB_TOKEN` | Docker Hub access token | As above |
| `RENDER_API_KEY` | Render API key, to validate the blueprint | The blueprint validation is skipped with a notice |
| `RENDER_WORKSPACE_ID` | Render workspace ID, to validate the blueprint | As above |

Render's deploy hook is still available in the dashboard for a manual redeploy, but nothing in
the pipeline needs it.

### Validating the blueprint

CI validates `render.yaml` through Render's own
[Validate Blueprint endpoint](https://api-docs.render.com/reference/validate-blueprint), which
checks the syntax and structure and reports the resources the Blueprint would create. A broken
Blueprint then fails a pull request check rather than a deployment.

The endpoint needs a Render API key and the workspace ID, so the job skips itself with a notice
until both are set as repository secrets. In Render, the key is under Workspace Settings, then
API Keys, and the workspace ID is under Workspace Settings, then General.

The Blueprint can also be validated locally against Render's published JSON Schema, which is
what the IDE's YAML extension uses:

```bash
curl -fsS https://render.com/schema/render.yaml.json -o schema.json
# then point your YAML extension at it, or use a JSON Schema validator
```

### Launching it on Render

The blueprint is the better way in, because it creates the database and wires `DATABASE_URL` for
you.

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

Keeping the native Go runtime works too, with these build settings instead:

| Setting | Value |
|---|---|
| Build Command | `go build -tags netgo -ldflags '-s -w' -o app ./cmd/api` |
| Start Command | `./app` |

Either way, make sure the service has `DATABASE_URL` pointing at a Postgres instance and
`CLINIC_TZ` set to `Africa/Nairobi`. Render injects `PORT` itself.

> **Keeping the service name matters.** The Blueprint adopts an existing service when its `name`
> matches exactly. `clinic-booking-system-1` is the name Render generated; if the Blueprint
> names it something else, Render creates a *second* service and leaves the original running.

### Upgrading the database

`postgresMajorVersion` in `render.yaml` applies when the database is created. It does not
migrate a database that already exists, so an instance created against the previous
`postgresMajorVersion` stays on the old major version.

Render documents two routes. An in-place upgrade always moves to the latest major version
Render supports, which is currently 18, and it requires downtime. Creating a new database on
18 and pointing the service at it is the option to take here: the database holds only seed data
and bookings created while testing, so there is nothing worth migrating. Create a fresh
PostgreSQL 18 instance, copy its **External Database URL** into the web service's
`DATABASE_URL`, and redeploy. The migrations and the seed run automatically on boot.

### Troubleshooting

`go: go.mod file not found in current directory or any parent directory`

Render deployed a commit that predates the module. Check that the branch it deploys from
actually contains `go.mod`. This is what happens when a pull request has not been merged into
`main` yet.

`Using Go version ...` followed by `go build -tags netgo`

The service is using Render's native Go runtime instead of Docker, so the `Dockerfile` is being
ignored.

`no Go files in ...` or `main.go: no such file or directory`

Same cause as above: the native runtime builds from the repository root, but the main package is
at `./cmd/api`. Either switch to the Docker runtime or set the build command above.

`failed to connect to ... database`

The `DATABASE_URL` is wrong or the database is not in the same region as the service. Check the
Render dashboard for the database's connection string and copy it into the service's environment
variables.

`The suggested container configuration for 18+ is to place a single mount at /var/lib/postgresql`

PostgreSQL 18's Docker image moved its data directory to `/var/lib/postgresql/18/docker` and
declares `/var/lib/postgresql` as the volume. A volume mounted at `/var/lib/postgresql/data`,
the PostgreSQL 16 layout, is rejected outright by the image. `docker-compose.yml` mounts the
parent directory for this reason.

`database "clinic_test" does not exist`

The integration tests need their own database. The compose stack creates `clinic`; create the
test one alongside it:

```bash
docker compose exec postgres psql -U postgres -c "CREATE DATABASE clinic_test;"
```

## Section 4: AI reflection

### What I used AI for

Across the four sections:

- Section 1 (design). I had the AI interrogate the brief before writing any code. It
  produced a list of ambiguities in the requirements: the missing timezone, the unstated
  appointment duration, the missing service layer, the reschedule race. I resolved each one by
  making an explicit decision and recording it in `AGENTS.md` and above.
- Section 2 (implementation). Mostly me writing Go, with AI assistance for the mechanical
  parts. It found the pgx error inspection for unique violations and drafted the `httptest`
  scaffolding; the slot queries I wrote myself.
- Section 3 (deployment). The AI drafted the `Dockerfile`, the `render.yaml` blueprint and
  the GitHub Actions workflow. I wrote the deployment steps by hand against Render's and
  Docker Hub's actual interfaces.
- Section 4 (this section). I asked the AI to interrogate the brief specifically for holes
  and contradictions, and it found several that I would not have found on my own.
- This README and `reference.md`. I ran both through the `blader/humanizer` skill, which
  catalogues the habits that make a passage read as machine-written: contrasts that only add
  weight, one-line closers that restate the paragraph above them, triads and dashes applied by
  habit rather than by meaning, and bold labels on every item of a list. Removing them changed
  the prose without changing a fact.

### A case where the AI improved my work

**Prompt:** *"Review this clinic booking API brief for holes, ambiguities and contradictions.
Focus on the schema, the concurrency guarantees and the test requirements."*

The most useful thing that came back was that the prescribed unique index does not actually
prevent double-booking.

The brief asked for a composite unique index on `(doctor_id, appointment_date, start_time)`. That
index prevents two appointments starting at the same instant. It says nothing about two
appointments that overlap. A 09:15 appointment inserted alongside a 09:00 appointment produces
no index violation at all, because 09:15 is a distinct key. Both bookings go through and the
doctor is double-booked.

The follow-on question, "when does the index actually suffice?", has a clean answer: when
every appointment is exactly 30 minutes long and every start lands on the 30-minute grid. Under
those two rules, overlap implies an identical start time, and the index catches it. The fix
was to make the duration fixed and the grid mandatory, which the brief had left as an unstated
assumption.

That observation changed the schema (a `CHECK` constraint pinning the duration), the service
layer (grid alignment became a validation rule rather than a convenience), and the test suite
(it became a testable property rather than an assumption). Reading the brief on my own, I would
not have spotted the gap, because the index in the brief looks like it does the job.

### Cases where the AI output was wrong

#### A test that asserted the opposite of the truth

I wrote a table-driven test for the slot grid with a case named "after the shift" expecting
`IsOnGrid(09:00, 17:00)` to be `false`. The grid is anchored at the start of the shift and
knows nothing about where the shift ends. 17:00 *is* on the grid, and it is the working-hours
check that rejects it. The test failed, and the fix was to correct the expectation and rename
the case to say what it actually means. I had written the test from my mental model of the
function.

#### A parser that was not as strict as the specification required

The brief says a time of day is `HH:MM`. I wrote `time.Parse("15:04", value)` and assumed that
enforced two-digit hours. It does not: Go's `15` layout accepts a single-digit hour, so `"9:00"`
parsed successfully as 09:00. The unit test for it failed, and the fix was to format the parsed
value back and compare it with the input, so `"9:00"` is rejected and `"09:00"` is accepted.

#### An assumption about the driver that was simply false

I wrote the repository expecting `pgx` to return `TIME` and `DATE` columns as `time.Time`, the
way most ORMs do. The first integration test against real PostgreSQL failed with `unsupported
Scan, storing driver.Value type string into type *time.Time`. Through `database/sql`, `pgx` hands
`TIME` back as the text `"09:00:00"`. The fix was to scan into `any` and convert explicitly,
accepting either representation. No unit test could have caught this. It only appeared once the
code ran against a real database, which is why the integration tests were worth writing.

#### A test assertion that was written backwards

The integration test for rescheduling asserted that the original slot was absent from the
availability list after a reschedule. A reschedule frees the original slot, so it has to be
present. The test failed against working code, and the fix was to invert the assertion. My
reading of the requirement was wrong.

#### A requirement ordering that produced a worse error message

The brief's validation order checked grid alignment before working hours, which meant a request
for 08:30 when the doctor starts at 09:00 was reported as "not on the 30-minute grid". That is
technically true, but it points at the wrong problem. I changed the order to check the working
window first and recorded the change in `AGENTS.md` rather than deviating from it silently.

### Two decisions I made without the AI

#### 1. Where the "this appointment has already started" rule belongs

The brief puts it inside the transaction, in the repository. I decided it belongs in the service
layer, which owns the business rules, with the repository re-checking it under the row lock as a
safety net. The reason is error quality: the service can read the appointment and report
*"appointment 3 has already started and cannot be cancelled"*, whereas a repository that can
only return a sentinel error forces the service into a generic message. The repository still
enforces the rule, so correctness does not depend on where the check lives; only the quality of
the response does.

#### 2. Treating "already cancelled" as `409 Conflict`

The request is well-formed and it is the resource's state that makes it impossible, which is
what `409` is for. `422` is reserved for requests that are well-formed but inherently
disallowed, such as a slot outside working hours. This is a judgement about REST semantics
rather than a technical fact.
