-- Each system adds its own people. A person is added by email, before they have an account ID, so
-- the subject can be empty until their first confirmed sign-in, and is kept after it.
ALTER TABLE person ALTER COLUMN mjolnir_subject DROP NOT NULL;
ALTER TABLE person ALTER COLUMN name SET DEFAULT '';
ALTER TABLE person ADD COLUMN added_at timestamptz;
ALTER TABLE person ADD COLUMN added_by bigint REFERENCES person;
-- Existing grants stay, and a system's owner by TVA's file keeps their way in. Anyone else who
-- signed in before this keeps their row, for the records, and is refused.
UPDATE person p SET added_at = now()
	WHERE EXISTS (SELECT 1 FROM role_grant g WHERE g.person_id = p.id AND g.ended_at IS NULL)
		OR EXISTS (SELECT 1 FROM caller c WHERE c.owner_subject = p.mjolnir_subject);
CREATE UNIQUE INDEX person_waiting_email ON person (lower(email))
	WHERE mjolnir_subject IS NULL AND added_at IS NOT NULL;
