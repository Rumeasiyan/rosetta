-- What R-REC-01 and R-REC-03 need beyond Annex A's first shape (docs/DECISION-LOG.md).
-- A signed-in officer may never have opened the screens, so has no person row: the account
-- is kept as CERBERUS named it, and person_id is filled where a row exists.
ALTER TABLE usage_record ADD COLUMN officer_account text;
-- A read of the gate or the languages is a request too, and carries no text.
ALTER TABLE usage_record DROP CONSTRAINT usage_record_operation_check;
ALTER TABLE usage_record ADD CONSTRAINT usage_record_operation_check
	CHECK (operation IN ('translate', 'grammar', 'memory add', 'test', 'read'));
-- Today's counts and the live part of the usage screens read by time.
CREATE INDEX usage_record_at ON usage_record (at);
CREATE INDEX usage_record_caller_at ON usage_record (caller_id, at);
-- Usage screens go by source too. A row with an empty source is the whole; a row naming one
-- counts the requests that source had a part in, so one request can be in several.
ALTER TABLE daily_total ADD COLUMN source text NOT NULL DEFAULT '';
ALTER TABLE daily_total DROP CONSTRAINT daily_total_pkey;
ALTER TABLE daily_total ADD PRIMARY KEY (day, caller_id, operation, from_language, to_language, outcome, source);
CREATE INDEX daily_total_day ON daily_total (day);
