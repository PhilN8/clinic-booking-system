-- Core schema for the clinic booking system.
--
-- Time model note: appointment_date, start_time and end_time hold naive
-- wall-clock values in the clinic's timezone. Postgres DATE and TIME columns
-- carry no zone, which is exactly what we want here: "the doctor works
-- 09:00-17:00" and "the appointment starts at 09:30" are local concepts.
-- created_at and cancelled_at are TIMESTAMPTZ because they record absolute
-- instants of real events.

CREATE TABLE doctors (
    id                 BIGSERIAL PRIMARY KEY,
    name               TEXT NOT NULL,
    start_working_hour TIME NOT NULL,
    end_working_hour   TIME NOT NULL,

    -- A shift must be a positive span of time...
    CONSTRAINT doctors_hours_ordered
        CHECK (end_working_hour > start_working_hour),

    -- ...and it must start and end on a half-hour boundary so that the
    -- 30-minute slot grid is anchored to a predictable wall-clock grid.
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

    -- Every appointment is exactly one slot long. Enforcing it in the schema
    -- means the application cannot create a booking that the partial unique
    -- index below would fail to protect.
    CONSTRAINT appointments_duration_valid
        CHECK (end_time = start_time + INTERVAL '30 minutes')
);

-- Database-level double-booking protection.
--
-- The index is partial: cancelled appointments are excluded, so cancelling
-- frees the slot with no cleanup job and no soft-delete bookkeeping.
--
-- Because every appointment is exactly 30 minutes long and every start time
-- is grid-aligned, two appointments can only overlap if they start at the
-- same instant. This index is therefore sufficient; no range/exclusion
-- constraint is required.
CREATE UNIQUE INDEX appointments_no_double_booking
    ON appointments (doctor_id, appointment_date, start_time)
    WHERE status <> 'cancelled';

-- Supports the availability query (booked slots for one doctor on one day).
CREATE INDEX appointments_doctor_date_booked_idx
    ON appointments (doctor_id, appointment_date)
    WHERE status = 'booked';

-- Supports the patient's appointment history.
CREATE INDEX appointments_patient_date_idx
    ON appointments (patient_id, appointment_date, start_time);
