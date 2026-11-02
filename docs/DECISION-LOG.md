# ROSETTA decision log

Build decisions the PRD and backlog do not hold. Newest first.

## One database role

One role, `rosetta`, as the platform template gives (allspark `apps/_template/database.sql`), not an owner and service pair. Nothing in ROSETTA needs an append-only table the service cannot change.

## Job names

`nightly`, `partitions`, `daily` and `audit-catch-up`. The TVA refresh runs inside `daily`, since flags depend on it.

## The screens' design is approved

The owner picked direction `stone` in `design-lab/`, "Stone bands": three flat bands of stone, olive and brown, one for each language, on warm paper; a heavy serif for terms; a plain system font for everything else; outlined pill controls. It takes its layout from a reference the owner chose, in ROSETTA's own colours. Tables stack at phone width. Screens copy `design-lab/src/directions/stone/`. Fonts are served from ROSETTA itself: nothing outside (R-NFR-11).
