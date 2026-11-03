-- A change to a calling system's limits is kept as history like any setting (R-ADM-05).
ALTER TABLE change_history DROP CONSTRAINT change_history_subject_check;
ALTER TABLE change_history ADD CONSTRAINT change_history_subject_check
	CHECK (subject IN ('setting', 'source order', 'source switch', 'caller limit'));
