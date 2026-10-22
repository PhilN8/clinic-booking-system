# Clinic Booking System

A REST API for a small clinic with five doctors. Patients see which 30-minute slots are free for
a doctor on a given day, book one, cancel it, or move it to a different slot. A booked slot can
never be double-booked.

Built in Go with PostgreSQL. Standard library only, plus the Postgres driver. The same binary
serves the patient-facing site.

| | |
|---|---|
| **Live site** | <https://clinic-booking-system-1-yu19.onrender.com/doctors> |
| **Live API** | <https://clinic-booking-system-1-yu19.onrender.com/api/doctors> |

This file answers sections 1, 3 and 4 of the assessment. The API reference, the local setup, the
test matrix, the deployment reference, the known limitations and the design system are in
[`reference.md`](reference.md).

## Section 1: system design

*The assessment asks for the models and components, the key decisions, and the trade-offs
considered. Those are below.*

### Models

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

Appointment times are naive wall-clock values in the clinic's timezone. The audit columns are
absolute instants. The split is deliberate: "the doctor works 09:00 to 17:00" is a local concept,
while "when was this row cancelled" is an event on a timeline.

### Components

```
HTTP request
    │
    ▼
internal/handlers      decode the request, call the service, encode the response
    ▼
internal/service       the clinic's rules: validity, grid, lead time, slot generation
    ▼
internal/repository    every SQL statement and every transaction
    ▼
PostgreSQL
```

The dependency arrow only points downwards. The service layer declares the persistence interface
it needs and the Postgres implementation satisfies it structurally, so the rules never depend on
the database. That is what lets the rule tests run without one.

Rules are pure functions over plain values, and the clock is injected rather than read from the
inside, so a test can pin it to an exact instant:

```go
func ValidateSlot(doctor *models.Doctor, date, start time.Time, now time.Time, loc *time.Location) error
```

The site (`internal/web`) sits alongside the API rather than above it. It reads through the same
service layer and renders Go templates, so it works without JavaScript and the JSON contract is
untouched. Three paths exist as both a page and an endpoint, and they negotiate on the `Accept`
header: a browser gets the page, `curl` gets JSON. The whole API is also mounted under `/api`,
where it is always JSON.

### Slots

A doctor's shift is divided into consecutive 30-minute slots anchored at the start of the shift.
A 09:00 to 17:00 shift gives 16 slots, from 09:00 through 16:30.

Three rules decide whether a slot can be booked. Working-hours bounds are inclusive, so a 09:00
start is valid and a 16:30 start is valid because it ends exactly at 17:00, but a 17:00 start is
not. Grid alignment is mandatory, so a 09:15 start is rejected. A slot must also be at least an
hour away from now, and a slot starting exactly 60 minutes away is allowed.

Grid alignment is what makes the database constraint sufficient. With a fixed 30-minute duration
and aligned starts, two appointments can only overlap if they start at the same instant, and this
index catches that:

```sql
CREATE UNIQUE INDEX appointments_no_double_booking
    ON appointments (doctor_id, appointment_date, start_time)
    WHERE status <> 'cancelled';
```

Two clients racing for one slot cannot both win: the second insert violates the index and is
reported as `409 SLOT_TAKEN`. Cancelling frees the slot with no cleanup job, because the index is
partial and the cancelled row leaves it. The API never checks availability and then inserts. It
inserts and lets the index decide.

### Rescheduling

A reschedule cancels the original and inserts a replacement, in one transaction:

```
BEGIN
  SELECT ... FOR UPDATE            -- lock the appointment row
  re-check status and start time
  UPDATE original SET status='cancelled', cancellation_reason='rescheduled'
  INSERT replacement SET status='booked', rescheduled_from_id=<original>
COMMIT
```

The row lock stops two concurrent reschedules of the same appointment from both cancelling it
and inserting two replacements. If the replacement hits a taken slot the transaction rolls back
and the original appointment is still booked.

### Time

The clinic operates in one timezone, `Africa/Nairobi` by default and configurable with
`CLINIC_TZ`. To compare a stored slot against now, the service builds the slot's absolute instant
with `time.Date(..., clinicLoc)`. That is exact because Nairobi has a fixed +03:00 offset and no
daylight saving, so the wall-clock model depends on that.

The same lead-time predicate is used by `POST /appointments` and by
`GET /doctors/{id}/availability`, which is why the API never offers a slot it would then refuse.

### Decisions and trade-offs

**PostgreSQL, not SQLite.** The brief's requirements are concurrency requirements, and
PostgreSQL is where they are testable: a partial unique index, a row lock, a transaction that
rolls back, two connections genuinely competing. The cost is a managed database in production and
a service container in CI. The benefit is that the central claim, that a slot cannot be
double-booked, is enforced by the database and shown by a test with eight goroutines racing for one
slot.

**Wall-clock times, not `TIMESTAMPTZ`.** The domain is local. Storing the times naively makes the
grid arithmetic trivial and the partial index obviously correct, and it makes the API's
`start_time: "09:30"` mean what it says. The trade-off is that the model assumes a single
DST-free zone, and a second clinic in another zone would need revisiting.

**A partial unique index, not application-level checks.** Checking whether a slot is free and then
inserting it leaves a race window, and the window is where double-bookings come from. The
application attempts the insert optimistically and translates the `23505` violation into `409`.
The `WHERE status <> 'cancelled'` clause is what makes cancelling free the slot without a cleanup
job. A range exclusion constraint would allow variable-length appointments; it was not used
because the duration is fixed, which makes identical start times the only possible overlap.

**The standard library router.** Go 1.22's `ServeMux` understands method-and-wildcard patterns
natively, so `GET /doctors/{id}/availability` needs no router dependency. The middleware this
service needs is about forty lines.

**Cancel and re-insert for rescheduling.** It preserves the history, and `rescheduled_from_id`
links the replacement to its predecessor. The cost is that the appointment id changes and the
patient sees two entries in their history, one cancelled and one booked.

**A service layer, with rules as pure functions.** The rules should be testable without a
database, and the injected clock lets the one-hour boundary be asserted exactly rather than
approximately.

**No authentication.** The brief does not ask for it and its scope is booking logic. Inventing a
user model and a session story would have left them untested against real requirements. The
honest gap is that the endpoints are unauthenticated and un-rate-limited, so anyone who can reach
the deployment can book and cancel.

**400 for unreadable requests, 422 for disallowed ones.** A malformed body or an unparseable date
is `400`. A date that parses but falls outside the doctor's hours is `422`. Making everything
`400` is simpler, but it collapses "you sent nonsense" and "that slot is not available" into the
same answer.

## Section 3: deployment and CI/CD

*The assessment asks for the public URL, the branch that triggers a deploy and how, and a brief
description of what the pipeline does.*

### What is deployed

Render runs the service as a Docker web service with a managed PostgreSQL 18 instance, from
`render.yaml` in this repository.

| | |
|---|---|
| **Live site** | <https://clinic-booking-system-1-yu19.onrender.com/doctors> |
| **Live API** | <https://clinic-booking-system-1-yu19.onrender.com/api/doctors> |
| **Health check** | <https://clinic-booking-system-1-yu19.onrender.com/healthz> |
| **Deploy trigger** | a push to `main` |
| **Database** | Render managed PostgreSQL 18, free plan |

### The pipeline

`.github/workflows/ci.yml` runs two jobs.

`test` runs on every pull request and every push to `main`: `go mod verify`, `go vet ./...`,
`go test -race ./...` with a `postgres:18` service container so the integration tests run rather
than skip, and `go build ./...`.

`publish` runs only on a push to `main`, and only if `test` passed. It builds the Docker image
and pushes it to Docker Hub, tagged with the commit SHA and `latest`. The job skips itself with a
notice when `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` are missing, so a misconfigured pipeline
never turns `main` red. Pull requests never touch the live environment.

### How a deploy is triggered

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

`autoDeployTrigger: checksPass` in `render.yaml` closes the loop. Render watches the linked
branch and waits for its CI checks to pass before it deploys, so a red test suite on `main` blocks
the release. The pipeline gates the deploy rather than running alongside it. The CI workflow
therefore does not also call Render's deploy hook, because that would deploy twice.

Two more secrets are optional. `RENDER_API_KEY` and `RENDER_WORKSPACE_ID` let CI validate
`render.yaml` against Render's own Validate Blueprint endpoint, so a broken blueprint fails a
pull request check rather than a deployment. Without them the step is skipped with a notice.

Operating detail, including how to launch the service on Render, how to upgrade the database and
what to do when a deploy fails, is in [`reference.md`](reference.md).

## Section 4: AI reflection

*The assessment asks what AI was used for across the four sections, one example where an AI
suggestion improved the work and the prompt behind it, one example where the output was wrong and
how it was caught, and two decisions made without AI.*

### 1. What I used AI for

**Section 1 (design).** I had the AI interrogate the brief before writing any code. It produced
a list of ambiguities: the missing timezone, the unstated appointment duration, the missing
service layer, the reschedule race. I resolved each one by making an explicit decision and
recording it in `AGENTS.md` and above.

**Section 2 (implementation).** Mostly me writing Go, with AI assistance for the mechanical
parts. It found the pgx error inspection for unique violations and drafted the `httptest`
scaffolding; the slot queries I wrote myself.

**Section 3 (deployment).** The AI drafted the `Dockerfile`, the `render.yaml` blueprint and the
GitHub Actions workflow. I wrote the deployment steps by hand against Render's and Docker Hub's
actual interfaces.

**Section 4 and the documentation.** I asked the AI to interrogate the brief for holes and
contradictions, which produced the insight in the next section. The mistakes in the one after it
were mine, and the tests caught them. I also ran both documentation files through the
`blader/humanizer` skill, which catalogues the habits that make a passage read as machine-written:
contrasts that only add weight, one-line closers that restate the paragraph above them, triads
and dashes applied by habit, and bold labels on every item of a list. Removing them changed the
prose without changing a fact.

### 2. Where it improved the work

**Prompt:** *"Review this clinic booking API brief for holes, ambiguities and contradictions.
Focus on the schema, the concurrency guarantees and the test requirements."*

The useful thing that came back was that the prescribed unique index does not prevent
double-booking. The brief asked for a composite unique index on `(doctor_id, appointment_date,
start_time)`, which prevents two appointments starting at the same instant and says nothing about
two that overlap. A 09:15 appointment inserted alongside a 09:00 one violates nothing, because
09:15 is a distinct key, and the doctor ends up double-booked.

The follow-on question has a clean answer. The index suffices when every appointment is exactly
30 minutes long and every start lands on the 30-minute grid, because then overlap implies an
identical start time. So the fix was to make the duration fixed and the grid mandatory, which
the brief had left as an unstated assumption. That changed the schema, the service layer and the
test suite, and I would not have spotted the gap on my own because the index in the brief looks
like it does the job.

### 3. Where it was wrong

**A test that asserted the opposite of the truth.** I wrote a table-driven case named "after the
shift" expecting `IsOnGrid(09:00, 17:00)` to be `false`. The grid is anchored at the start of
the shift and knows nothing about where the shift ends, so 17:00 is on the grid and the
working-hours check is what rejects it. The test failed, and I corrected the expectation and
renamed the case to say what it actually means.

**A parser that was not as strict as the specification required.** The brief says a time of day is
`HH:MM`. I wrote `time.Parse("15:04", value)` and assumed that enforced two-digit hours. Go's
`15` layout accepts a single-digit hour, so `"9:00"` parsed as 09:00. The unit test failed, and
the fix was to format the parsed value back and compare it with the input.

**An assumption about the driver that was simply false.** I expected `pgx` to return `TIME` and
`DATE` columns as `time.Time`, the way most ORMs do. The first integration test against real
PostgreSQL failed with `unsupported Scan, storing driver.Value type string into type *time.Time`,
because through `database/sql` `pgx` hands `TIME` back as the text `"09:00:00"`. No unit test
could have caught it. It only appeared once the code ran against a real database, which is why
the integration tests were worth writing.

### 4. Two decisions I made without the AI

**Where the "this appointment has already started" rule belongs.** The brief puts it in the
repository, inside the transaction. I put it in the service layer, which owns the business rules,
with the repository re-checking it under the row lock as a safety net. The reason is error
quality: the service can report "appointment 3 has already started and cannot be cancelled",
whereas a repository that returns only a sentinel error forces a generic message. Correctness
does not depend on where the check lives; the quality of the response does.

**Treating "already cancelled" as `409 Conflict` rather than `422 Unprocessable Entity`.** The
request is well-formed and the resource's state is what makes it impossible, which is what `409`
is for. `422` is for requests that are well-formed but inherently disallowed, such as a slot
outside working hours. This is a judgement about REST semantics rather than a technical fact, and
I would rather defend it than have it made by default.
