-- A notice to keepers is an alert like any other HEDWIG carries (Annex A), so what could not go
-- waits here for the next try, and says what it is about. It holds no word of the glossary.
ALTER TABLE alert ADD COLUMN glossary_entry_id bigint REFERENCES glossary_entry;
ALTER TABLE alert ADD COLUMN glossary_import_id bigint REFERENCES glossary_import;
CREATE INDEX alert_unsent ON alert (id) WHERE sent_at IS NULL;
