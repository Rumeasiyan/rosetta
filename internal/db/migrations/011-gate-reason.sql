-- An opening says why it was made and whether a native reader checked the test set
-- (R-GAT-01). No row was open before this, so the rule holds for all of them.
ALTER TABLE gate_decision
	ADD COLUMN reason text,
	ADD COLUMN test_set_checked boolean NOT NULL DEFAULT false,
	ADD CONSTRAINT gate_decision_open_has_reason CHECK (state = 'closed' OR reason IS NOT NULL);
