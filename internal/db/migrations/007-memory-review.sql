-- Finding memory entries by text looks in both sides (R-MEM-05, R-GLO-06).
CREATE INDEX memory_entry_approved ON memory_entry USING gin (approved_text gin_trgm_ops);
-- What waits for a reviewer, and the glossary entry that sent it there (R-MEM-04).
CREATE INDEX memory_entry_recheck ON memory_entry (to_language, id) WHERE state = 'recheck';
ALTER TABLE memory_entry ADD COLUMN recheck_glossary_entry_id bigint REFERENCES glossary_entry;
ALTER TABLE memory_entry ADD COLUMN recheck_glossary_version integer REFERENCES glossary_version;
