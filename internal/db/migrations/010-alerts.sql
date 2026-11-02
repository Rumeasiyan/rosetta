-- What an alert is about, past a system or a source: a direction, a language or a person's
-- reference, never a name or a word of text.
ALTER TABLE alert ADD COLUMN subject text;
-- The last state alerted on, so one alert goes when a state changes, not one per failed request
-- (E-ADM-06). It outlives a restart, so starting again tells nobody twice.
CREATE TABLE alert_state (
	key        text PRIMARY KEY,
	value      text NOT NULL,
	changed_at timestamptz NOT NULL DEFAULT now()
);
