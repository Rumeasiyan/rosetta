-- One correction of an entry waits at a time, so publishing it can never replace an entry
-- already replaced (R-GLO-02).
CREATE UNIQUE INDEX glossary_entry_one_correction ON glossary_entry (replaces_id)
	WHERE status IN ('proposed', 'held');
CREATE INDEX glossary_entry_status ON glossary_entry (status);
-- A published term is found again by language and text, whichever way it was typed.
CREATE INDEX glossary_term_same ON glossary_term (language, lower(text));
