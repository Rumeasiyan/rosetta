-- An entry PENSIEVE refuses for good stays, marked, and is not sent again: sending
-- it again gets the same answer, so a person must fix what wrote it (docs/INTAKE.md
-- in PENSIEVE's repository). The reason holds PENSIEVE's words, never the entry's.
ALTER TABLE audit_outbox ADD COLUMN refused_at timestamptz;
ALTER TABLE audit_outbox ADD COLUMN refusal text;
