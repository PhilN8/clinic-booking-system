-- Seed data so a reviewer can hit the API immediately without creating
-- doctors and patients first. IDs are explicit and stable.
--
-- The setval calls are required: inserting explicit ids into a BIGSERIAL
-- column does not advance the sequence, so without them the first
-- application-inserted row would collide with a seeded one.

INSERT INTO doctors (id, name, start_working_hour, end_working_hour) VALUES
    (1, 'Dr. Amara Ochieng',  '09:00', '17:00'),
    (2, 'Dr. Brian Otieno',   '08:00', '16:00'),
    (3, 'Dr. Chloe Mwangi',   '10:00', '18:00'),
    (4, 'Dr. Daniel Kimani',  '09:00', '13:00'),
    (5, 'Dr. Esther Wanjiku', '13:00', '17:00')
ON CONFLICT (id) DO NOTHING;

SELECT setval(pg_get_serial_sequence('doctors', 'id'), (SELECT MAX(id) FROM doctors));

INSERT INTO patients (id, name) VALUES
    (1, 'Grace Wanjiku'),
    (2, 'Samuel Otieno')
ON CONFLICT (id) DO NOTHING;

SELECT setval(pg_get_serial_sequence('patients', 'id'), (SELECT MAX(id) FROM patients));
