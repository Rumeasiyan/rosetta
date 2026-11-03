-- Every table in Annex A of docs/ROSETTA-PRD.md; docs/diagrams/build-erd.py holds
-- the same list. An applied migration is never edited, only followed by a new one.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE person (
	id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	mjolnir_subject text NOT NULL UNIQUE,
	name            text NOT NULL,
	email           text NOT NULL,
	screen_language text NOT NULL DEFAULT 'en' CHECK (screen_language IN ('si', 'ta', 'en')),
	first_seen_at   timestamptz NOT NULL DEFAULT now(),
	last_seen_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE role_grant (
	id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	person_id  bigint NOT NULL REFERENCES person,
	role       text NOT NULL CHECK (role IN ('administrator', 'glossary keeper', 'memory reviewer')),
	language   text CHECK (language IN ('si', 'ta', 'en')),
	post_ref   text NOT NULL DEFAULT 'not known',
	starts_at  timestamptz NOT NULL DEFAULT now(),
	ends_on    date,
	ended_at   timestamptz,
	ended_by   bigint REFERENCES person,
	granted_by bigint REFERENCES person,
	CHECK ((role = 'administrator') = (language IS NULL))
);
CREATE INDEX role_grant_live ON role_grant (person_id) WHERE ended_at IS NULL;

CREATE TABLE setting (
	name       text PRIMARY KEY,
	value      text NOT NULL,
	updated_at timestamptz NOT NULL DEFAULT now(),
	updated_by bigint REFERENCES person
);

CREATE TABLE change_history (
	id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	subject     text NOT NULL CHECK (subject IN ('setting', 'source order', 'source switch')),
	subject_ref text NOT NULL,
	old_value   text,
	new_value   text,
	changed_at  timestamptz NOT NULL DEFAULT now(),
	changed_by  bigint NOT NULL REFERENCES person
);

CREATE TABLE paradox_run (
	run_id      text PRIMARY KEY,
	job         text NOT NULL,
	started_at  timestamptz NOT NULL DEFAULT now(),
	finished_at timestamptz
);

CREATE TABLE caller (
	id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	tva_reference       text NOT NULL UNIQUE,
	name                text NOT NULL CHECK (char_length(name) <= 200),
	owner_post_ref      text,
	owner_subject       text,
	tva_status          text NOT NULL,
	tva_checked_at      timestamptz NOT NULL,
	requests_per_minute integer NOT NULL DEFAULT 60 CHECK (requests_per_minute > 0),
	requests_at_once    integer NOT NULL DEFAULT 5 CHECK (requests_at_once > 0),
	registered_at       timestamptz NOT NULL DEFAULT now(),
	registered_by       bigint REFERENCES person,
	last_request_at     timestamptz,
	flag                text NOT NULL DEFAULT 'none'
		CHECK (flag IN ('none', 'no owner', 'idle', 'not live or may join'))
);

CREATE TABLE caller_key (
	id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	caller_id     bigint NOT NULL REFERENCES caller,
	key_hash      text NOT NULL UNIQUE,
	prefix        text NOT NULL UNIQUE,
	public_key    text,
	created_at    timestamptz NOT NULL DEFAULT now(),
	created_by    bigint REFERENCES person,
	last_used_at  timestamptz,
	turned_off_at timestamptz,
	turned_off_by bigint REFERENCES person
);
CREATE INDEX caller_key_live ON caller_key (caller_id) WHERE turned_off_at IS NULL;

CREATE TABLE caller_origin (
	caller_id bigint NOT NULL REFERENCES caller,
	origin    text NOT NULL,
	added_at  timestamptz NOT NULL DEFAULT now(),
	added_by  bigint REFERENCES person,
	PRIMARY KEY (caller_id, origin)
);
CREATE INDEX caller_origin_origin ON caller_origin (origin);

CREATE TABLE source (
	id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	name           text NOT NULL UNIQUE,
	version        text NOT NULL,
	state          text NOT NULL CHECK (state IN ('on', 'resting-block', 'resting-change', 'off')),
	rest_until     timestamptz,
	on_for_rosetta boolean NOT NULL DEFAULT true,
	switched_at    timestamptz,
	switched_by    bigint REFERENCES person,
	seen_at        timestamptz NOT NULL,
	processes_text text NOT NULL DEFAULT 'outside public website'
);

CREATE TABLE source_offer (
	id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	source_id       bigint NOT NULL REFERENCES source,
	operation       text NOT NULL CHECK (operation IN ('translate', 'grammar')),
	from_language   text NOT NULL CHECK (from_language IN ('si', 'ta', 'en')),
	to_language     text CHECK (to_language IN ('si', 'ta', 'en')),
	benchmark_score numeric,
	CHECK ((operation = 'translate') = (to_language IS NOT NULL)),
	CHECK (to_language IS DISTINCT FROM from_language),
	UNIQUE NULLS NOT DISTINCT (source_id, operation, from_language, to_language)
);

CREATE TABLE source_order (
	operation     text NOT NULL CHECK (operation IN ('translate', 'grammar')),
	from_language text NOT NULL CHECK (from_language IN ('si', 'ta', 'en')),
	to_language   text NOT NULL CHECK (to_language IN ('si', 'ta', 'en')),
	position      integer NOT NULL CHECK (position >= 1),
	source_id     bigint NOT NULL REFERENCES source,
	set_at        timestamptz NOT NULL DEFAULT now(),
	set_by        bigint REFERENCES person,
	PRIMARY KEY (operation, from_language, to_language, position),
	UNIQUE (operation, from_language, to_language, source_id)
);

CREATE TABLE gate_decision (
	id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	source_id        bigint NOT NULL REFERENCES source,
	from_language    text NOT NULL CHECK (from_language IN ('si', 'ta', 'en')),
	to_language      text NOT NULL CHECK (to_language IN ('si', 'ta', 'en')),
	source_version   text NOT NULL,
	state            text NOT NULL CHECK (state IN ('open', 'closed')),
	test_set_version text,
	score            numeric,
	evaluated_on     date,
	effective_from   timestamptz NOT NULL DEFAULT now(),
	effective_to     timestamptz,
	decided_by       bigint NOT NULL REFERENCES person,
	CHECK (from_language <> to_language),
	-- A direction opens only on a recorded evaluation (R-GAT-01).
	CHECK (state = 'closed' OR (test_set_version IS NOT NULL AND score IS NOT NULL AND evaluated_on IS NOT NULL))
);
CREATE UNIQUE INDEX gate_decision_current ON gate_decision (source_id, from_language, to_language)
	WHERE effective_to IS NULL;

CREATE TABLE glossary_version (
	number       integer PRIMARY KEY,
	published_at timestamptz NOT NULL DEFAULT now(),
	published_by bigint REFERENCES person,
	summary      text NOT NULL DEFAULT ''
);
-- The empty glossary, so the first translation has a version to record (R-TRN-06).
INSERT INTO glossary_version (number, summary) VALUES (0, 'empty');

CREATE TABLE glossary_import (
	id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	file_name   text NOT NULL,
	rows        integer NOT NULL,
	accepted    integer NOT NULL,
	refused     integer NOT NULL,
	imported_at timestamptz NOT NULL DEFAULT now(),
	imported_by bigint NOT NULL REFERENCES person
);

CREATE TABLE glossary_entry (
	id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	type               text NOT NULL CHECK (type IN ('post', 'institution', 'place', 'term', 'keep as written')),
	note               text NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
	status             text NOT NULL CHECK (status IN ('proposed', 'held', 'published', 'replaced', 'refused')),
	replaces_id        bigint REFERENCES glossary_entry,
	glossary_import_id bigint REFERENCES glossary_import,
	published_in       integer REFERENCES glossary_version,
	replaced_in        integer REFERENCES glossary_version,
	proposed_at        timestamptz NOT NULL DEFAULT now(),
	proposed_by        bigint REFERENCES person,
	outcome_note       text
);

CREATE TABLE glossary_term (
	glossary_entry_id bigint NOT NULL REFERENCES glossary_entry,
	language          text NOT NULL CHECK (language IN ('si', 'ta', 'en')),
	text              text NOT NULL CHECK (char_length(text) <= 500),
	checked_at        timestamptz,
	checked_by        bigint REFERENCES person,
	PRIMARY KEY (glossary_entry_id, language)
);
CREATE INDEX glossary_term_text ON glossary_term USING gin (text gin_trgm_ops);

CREATE TABLE memory_entry (
	id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	caller_id        bigint NOT NULL REFERENCES caller,
	glossary_version integer NOT NULL REFERENCES glossary_version,
	from_language    text NOT NULL CHECK (from_language IN ('si', 'ta', 'en')),
	to_language      text NOT NULL CHECK (to_language IN ('si', 'ta', 'en')),
	source_text      text NOT NULL,
	source_hash      text NOT NULL,
	approved_text    text NOT NULL,
	approver_post    text NOT NULL,
	approved_on      date NOT NULL,
	state            text NOT NULL DEFAULT 'served' CHECK (state IN ('served', 'recheck', 'removed', 'replaced')),
	replaced_by_id   bigint REFERENCES memory_entry,
	added_at         timestamptz NOT NULL DEFAULT now(),
	reviewed_at      timestamptz,
	reviewed_by      bigint REFERENCES person,
	removed_at       timestamptz,
	removed_by       bigint REFERENCES person,
	CHECK (from_language <> to_language)
);
-- One live entry per source piece: a newer approval replaces the older (R-MEM-02).
CREATE UNIQUE INDEX memory_entry_live ON memory_entry (source_hash, from_language, to_language)
	WHERE state IN ('served', 'recheck');
CREATE INDEX memory_entry_text ON memory_entry USING gin (source_text gin_trgm_ops);

-- UNLOGGED: may be empty after a crash or a restore, and refills (Annex A.1).
CREATE UNLOGGED TABLE cache_piece (
	source_hash      text NOT NULL,
	from_language    text NOT NULL CHECK (from_language IN ('si', 'ta', 'en')),
	to_language      text NOT NULL CHECK (to_language IN ('si', 'ta', 'en')),
	source_id        bigint NOT NULL REFERENCES source,
	glossary_version integer NOT NULL REFERENCES glossary_version,
	source_text      text NOT NULL,
	translation      text NOT NULL,
	source_version   text NOT NULL,
	created_at       timestamptz NOT NULL DEFAULT now(),
	expires_at       timestamptz NOT NULL,
	PRIMARY KEY (source_hash, from_language, to_language, source_id)
);
CREATE INDEX cache_piece_text ON cache_piece USING gin (source_text gin_trgm_ops);
CREATE INDEX cache_piece_expires ON cache_piece (expires_at);

-- Monthly parts, so the oldest month is dropped whole (R-REC-04). PostgreSQL needs
-- the partition key in every unique index, so request_ref is unique with at.
CREATE TABLE usage_record (
	id              bigint GENERATED ALWAYS AS IDENTITY,
	caller_id       bigint NOT NULL REFERENCES caller,
	person_id       bigint REFERENCES person,
	request_ref     text NOT NULL,
	at              timestamptz NOT NULL DEFAULT now(),
	operation       text NOT NULL CHECK (operation IN ('translate', 'grammar', 'memory add', 'test')),
	sensitivity     text CHECK (sensitivity IN ('ordinary', 'personal', 'confidential')),
	from_language   text,
	to_language     text,
	passages        integer NOT NULL DEFAULT 0,
	characters      integer NOT NULL DEFAULT 0,
	pieces_glossary integer NOT NULL DEFAULT 0,
	pieces_memory   integer NOT NULL DEFAULT 0,
	pieces_cache    integer NOT NULL DEFAULT 0,
	pieces_source   integer NOT NULL DEFAULT 0,
	sources_used    text[] NOT NULL DEFAULT '{}',
	fallback_used   boolean NOT NULL DEFAULT false,
	mismatch        boolean NOT NULL DEFAULT false,
	duration_ms     integer NOT NULL DEFAULT 0,
	outcome         text NOT NULL,
	PRIMARY KEY (id, at),
	UNIQUE (request_ref, at)
) PARTITION BY RANGE (at);

-- This month's part and the next; E-REC-03 adds later ones.
DO $$
DECLARE
	first date := date_trunc('month', now() AT TIME ZONE 'UTC')::date;
	i int;
	start date;
BEGIN
	FOR i IN 0..1 LOOP
		start := first + make_interval(months => i);
		EXECUTE format(
			'CREATE TABLE usage_record_%s PARTITION OF usage_record FOR VALUES FROM (%L) TO (%L)',
			to_char(start, 'YYYY_MM'), start::timestamptz, (start + interval '1 month')::timestamptz);
	END LOOP;
END $$;

CREATE TABLE daily_total (
	day           date NOT NULL,
	caller_id     bigint NOT NULL REFERENCES caller,
	operation     text NOT NULL,
	from_language text NOT NULL DEFAULT '',
	to_language   text NOT NULL DEFAULT '',
	outcome       text NOT NULL,
	requests      bigint NOT NULL DEFAULT 0,
	characters    bigint NOT NULL DEFAULT 0,
	pieces_glossary bigint NOT NULL DEFAULT 0,
	pieces_memory bigint NOT NULL DEFAULT 0,
	pieces_cache  bigint NOT NULL DEFAULT 0,
	pieces_source bigint NOT NULL DEFAULT 0,
	mismatches    bigint NOT NULL DEFAULT 0,
	time_buckets  jsonb NOT NULL DEFAULT '{}',
	PRIMARY KEY (day, caller_id, operation, from_language, to_language, outcome)
);

CREATE TABLE audit_outbox (
	id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	entry_id    text NOT NULL UNIQUE,
	occurred_at timestamptz NOT NULL DEFAULT now(),
	actor_id    bigint REFERENCES person,
	action      text NOT NULL,
	details     jsonb NOT NULL DEFAULT '{}',
	attempts    integer NOT NULL DEFAULT 0,
	next_try_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_outbox_due ON audit_outbox (next_try_at);

CREATE TABLE alert (
	id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	kind             text NOT NULL,
	caller_id        bigint REFERENCES caller,
	source_id        bigint REFERENCES source,
	raised_at        timestamptz NOT NULL DEFAULT now(),
	sent_at          timestamptz,
	hedwig_notice_id text
);
