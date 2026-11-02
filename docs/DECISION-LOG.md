# ROSETTA decision log

Build decisions the PRD and backlog do not hold. Newest first.

## One database role

One role, `rosetta`, as the platform template gives (allspark `apps/_template/database.sql`), not an owner and service pair. Nothing in ROSETTA needs an append-only table the service cannot change.

## Job names

`nightly`, `partitions`, `daily` and `audit-catch-up`. The TVA refresh runs inside `daily`, since flags depend on it.
