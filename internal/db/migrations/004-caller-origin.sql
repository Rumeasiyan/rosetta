-- A browser address counts to one system (R-CAL-06), so it can be listed only once, and a
-- change to the list is kept as history like any setting (R-ADM-05).
DROP INDEX caller_origin_origin;
CREATE UNIQUE INDEX caller_origin_origin ON caller_origin (origin);
ALTER TABLE change_history DROP CONSTRAINT change_history_subject_check;
ALTER TABLE change_history ADD CONSTRAINT change_history_subject_check
	CHECK (subject IN ('setting', 'source order', 'source switch', 'caller limit', 'caller origin'));
