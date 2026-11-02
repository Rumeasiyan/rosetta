"""Build ROSETTA's entity relationship diagram and data dictionary.

One list of entities and links feeds both outputs, so the picture and the
dictionary cannot drift apart:

- docs/diagrams/rosetta-erd.svg (and .dot), drawn with Graphviz;
- the data dictionary in docs/ROSETTA-PRD.md, between the markers
  <!-- dictionary:start --> and <!-- dictionary:end -->.

Run from the repository root:

    python3 docs/diagrams/build-erd.py

Needs Python 3.11 or newer and Graphviz (`dot` on the PATH; on macOS:
brew install graphviz).
"""

import pathlib
import re
import subprocess

ROOT = pathlib.Path(__file__).resolve().parents[2]
OUT = ROOT / "docs" / "diagrams"
PRD = ROOT / "docs" / "ROSETTA-PRD.md"
NAME = "rosetta-erd"
HUB = "person"
TOP = ["person", "caller", "source"]
# Tables without links kept level with their neighbours instead of sinking.
SAME_RANK = [("setting", "paradox_run")]
# Invisible links (lower, upper) that stack tables to fit an A3 landscape page.
STACK = [
    ("glossary_version", "memory_entry"),
    ("glossary_import", "cache_piece"),
    ("setting", "role_grant"),
    ("paradox_run", "change_history"),
]

# Subject areas: (name, border colour, fill colour)
AREAS = {
    "people": ("People, roles and settings", "#1d4ed8", "#eff6ff"),
    "callers": ("Callers and keys", "#b45309", "#fffbeb"),
    "sources": ("Sources and gate", "#047857", "#ecfdf5"),
    "glossary": ("Glossary", "#6d28d9", "#f5f3ff"),
    "memory": ("Memory and cache", "#0f766e", "#f0fdfa"),
    "records": ("Records and alerts", "#475569", "#f8fafc"),
}

LANG = "si, ta or en"

# Each entity: area, keys [(kind, name)], other fields [(name, description)].
# Key kinds: "PK", "FK", "PK FK".
ENTITIES = {
    "person": (
        "people",
        [("PK", "id")],
        [
            ("mjolnir_subject", "text, unique; the person's MJOLNIR account ID"),
            ("name, email", "text, as MJOLNIR sent them at the last sign-in"),
            ("screen_language", f"{LANG}; chosen on the screens and remembered (R-NFR-05)"),
            ("first_seen_at, last_seen_at", "time (UTC)"),
        ],
    ),
    "role_grant": (
        "people",
        [("PK", "id"), ("FK", "person_id"), ("FK", "granted_by")],
        [
            (
                "role",
                "administrator, glossary keeper or memory reviewer (R-ADM-06); never system owner",
            ),
            ("language", f"{LANG}; glossary keeper and memory reviewer only"),
            (
                "post_ref",
                "text; post and office at grant, from SCROLL; role ends when no longer held",
            ),
            ("starts_at, ends_on", "time (UTC); date or empty for no end date"),
            ("ended_at, ended_by", "time (UTC); person; ending takes effect at once"),
        ],
    ),
    "setting": (
        "people",
        [("PK", "name"), ("FK", "updated_by")],
        [
            ("value", 'text; every "(default: ...)" value in the PRD'),
            ("updated_at", "time (UTC); takes effect within 60 seconds (R-ADM-05)"),
        ],
    ),
    "change_history": (
        "people",
        [("PK", "id"), ("FK", "changed_by")],
        [
            ("subject", "setting, source order or source switch (R-ADM-05)"),
            ("subject_ref", "text; the setting name, or the operation and direction"),
            ("old_value, new_value", "text; never text sent by a caller"),
            ("changed_at", "time (UTC)"),
        ],
    ),
    "paradox_run": (
        "people",
        [("PK", "run_id")],
        [
            (
                "job",
                "nightly totals and removals, monthly record parts, daily flags, "
                "TVA refresh or audit catch-up",
            ),
            ("started_at, finished_at", "time (UTC); a run_id already here is skipped"),
            ("kept", "90 days"),
        ],
    ),
    "caller": (
        "callers",
        [("PK", "id"), ("FK", "registered_by")],
        [
            ("tva_reference", "text, unique; the system's entry in TVA (R-CAL-01)"),
            ("name", "text (max 200); display copy, refreshed daily from TVA's published file"),
            (
                "owner_post_ref, owner_subject",
                "copies from TVA's file; subject empty if vacant or no linked sign-in (R-CAL-02)",
            ),
            (
                "tva_status, tva_checked_at",
                "as TVA gives it; time (UTC); a caller neither live nor may join is refused",
            ),
            ("requests_per_minute", "number (default: 60) (R-CAL-07)"),
            ("requests_at_once", "number (default: 5)"),
            ("registered_at, last_request_at", "time (UTC)"),
            (
                "flag",
                "none, no owner, idle 90 days or not live or may join (R-CAL-02, R-CAL-05)",
            ),
        ],
    ),
    "caller_key": (
        "callers",
        [("PK", "id"), ("FK", "caller_id"), ("FK", "created_by"), ("FK", "turned_off_by")],
        [
            ("key_hash", "one-way hash; the key itself is shown once (R-CAL-03)"),
            ("prefix", "first characters, shown in lists"),
            ("public_key", "text; checks the signature from another machine; empty on this server"),
            ("created_at, last_used_at", "time (UTC)"),
            ("turned_off_at", "time (UTC); empty while live; at most 2 live per caller"),
        ],
    ),
    "caller_origin": (
        "callers",
        [("PK FK", "caller_id"), ("PK", "origin"), ("FK", "added_by")],
        [
            (
                "origin",
                "browser address allowed to call the grammar check, for example "
                "the letter editor's (R-CAL-06)",
            ),
            ("added_at", "time (UTC)"),
        ],
    ),
    "source": (
        "sources",
        [("PK", "id"), ("FK", "switched_by")],
        [
            ("name", "text, unique, as in BUMBLEBEE's list, for example google-translate"),
            ("version", "text; BUMBLEBEE's part for this site, returned with every answer"),
            ("state", "on, resting after a block, resting after a page change, or off"),
            ("rest_until", "time (UTC); empty unless resting; skipped until then (R-PRV-01)"),
            ("on_for_rosetta", "yes/no; the administrator can turn a source off (R-PRV-02)"),
            ("switched_at", "time (UTC)"),
            ("seen_at", "time (UTC); last refresh of the list, never older than 60 seconds"),
            ("processes_text", "always an outside public website for BUMBLEBEE sources"),
        ],
    ),
    "source_offer": (
        "sources",
        [("PK", "id"), ("FK", "source_id")],
        [
            ("operation", "translate or grammar check"),
            ("from_language", LANG),
            ("to_language", f"{LANG}; translation only, never the same as from_language"),
            (
                "benchmark_score",
                "grammar only; found share less false-alarm share, from BUMBLEBEE's "
                "latest run (R-GRM-06)",
            ),
        ],
    ),
    "source_order": (
        "sources",
        [
            ("PK", "operation"),
            ("PK", "from_language"),
            ("PK", "to_language"),
            ("PK", "position"),
            ("FK", "source_id"),
            ("FK", "set_by"),
        ],
        [
            ("operation", "translate or grammar check"),
            ("to_language", f"{LANG}; for grammar the same as from_language"),
            ("position", "number from 1; grammar follows benchmark score unless set (R-GRM-06)"),
            ("set_at", "time (UTC)"),
        ],
    ),
    "gate_decision": (
        "sources",
        [("PK", "id"), ("FK", "source_id"), ("FK", "decided_by")],
        [
            ("from_language, to_language", LANG),
            ("source_version", "text; a new version starts closed (R-GAT-01)"),
            ("state", "open or closed"),
            ("test_set_version, score", "text; number; the evaluation it rests on"),
            ("evaluated_on", "date"),
            ("effective_from, effective_to", "time (UTC); empty to while current; history kept"),
        ],
    ),
    "glossary_entry": (
        "glossary",
        [
            ("PK", "id"),
            ("FK", "replaces_id"),
            ("FK", "glossary_import_id"),
            ("FK", "published_in"),
            ("FK", "proposed_by"),
        ],
        [
            (
                "type",
                'post, institution, place, term, or "keep as written" (R-GLO-01)',
            ),
            ("note", "text (max 500)"),
            (
                "status",
                "proposed, held (not all three checked), published, replaced or refused "
                "(R-GLO-02); never deleted",
            ),
            ("proposed_at", "time (UTC)"),
            ("outcome_note", "text; shown to the proposer (R-GLO-04)"),
            ("replaced_in", "glossary version number; empty while current"),
        ],
    ),
    "glossary_term": (
        "glossary",
        [("PK FK", "glossary_entry_id"), ("PK", "language"), ("FK", "checked_by")],
        [
            ("language", LANG),
            ("text", "text (max 500), Unicode, normalised"),
            ("checked_at", "time (UTC); empty until the keeper for the language checks it"),
        ],
    ),
    "glossary_version": (
        "glossary",
        [("PK", "number"), ("FK", "published_by")],
        [
            ("published_at", "time (UTC); one number for the whole list (R-GLO-05)"),
            ("summary", "entries added and replaced"),
        ],
    ),
    "glossary_import": (
        "glossary",
        [("PK", "id"), ("FK", "imported_by")],
        [
            ("file_name", "text; spreadsheet of up to 5,000 rows (R-GLO-07)"),
            ("rows, accepted, refused", "numbers"),
            ("imported_at", "time (UTC)"),
        ],
    ),
    "memory_entry": (
        "memory",
        [
            ("PK", "id"),
            ("FK", "caller_id"),
            ("FK", "glossary_version"),
            ("FK", "replaced_by_id"),
            ("FK", "reviewed_by"),
            ("FK", "removed_by"),
        ],
        [
            ("from_language, to_language", LANG),
            ("source_text, source_hash", "text, normalised; hash for look-up, ordinary text only"),
            ("approved_text", "text; approved in the calling system (R-MEM-01)"),
            ("approver_post", "text, as the calling system sent it"),
            ("approved_on", "date"),
            (
                "state",
                "served, recheck, removed or replaced (R-MEM-02, R-MEM-04); "
                "found for recheck by text search",
            ),
            ("added_at, reviewed_at, removed_at", "time (UTC)"),
            ("kept", "until replaced or removed"),
        ],
    ),
    "cache_piece": (
        "memory",
        [
            ("PK", "source_hash"),
            ("PK", "from_language"),
            ("PK", "to_language"),
            ("PK FK", "source_id"),
            ("FK", "glossary_version"),
        ],
        [
            ("source_text, translation", "text; ordinary text only, never personal (R-TRN-08)"),
            ("source_version", "text"),
            ("created_at, expires_at", "time (UTC); 30 days"),
            ("kept", "30 days; may be empty after a crash or a restore, then rebuilt"),
        ],
    ),
    "usage_record": (
        "records",
        [("PK", "id"), ("FK", "caller_id"), ("FK", "person_id")],
        [
            ("request_ref", "text, unique; passed to every BUMBLEBEE call (R-REC-01)"),
            ("at", "time (UTC)"),
            ("operation", "translate, grammar check, memory add or test"),
            ("sensitivity", "ordinary, personal or confidential"),
            ("from_language, to_language", LANG),
            ("passages, characters", "numbers"),
            ("pieces_glossary, pieces_memory, pieces_cache, pieces_source", "numbers"),
            ("sources_used, fallback_used", "source names; yes/no"),
            ("mismatch", "yes/no; ordinary text held an identity card number (R-MSK-04)"),
            ("duration_ms", "milliseconds"),
            ("outcome", "answered, or one reason from R-ERR-01"),
            (
                "kept",
                "2 years in monthly parts, the oldest dropped; never any text; "
                "about 50 million rows (about 20 GB) at full capacity",
            ),
        ],
    ),
    "daily_total": (
        "records",
        [
            ("PK", "day"),
            ("PK FK", "caller_id"),
            ("PK", "operation"),
            ("PK", "from_language"),
            ("PK", "to_language"),
            ("PK", "outcome"),
        ],
        [
            ("requests, characters", "numbers"),
            ("pieces by origin", "glossary, memory, cache and source counts"),
            ("mismatches", "number"),
            ("time_buckets", "counts by time taken, for the usage screens (R-REC-03)"),
            ("kept", "5 years (R-REC-04)"),
        ],
    ),
    "audit_outbox": (
        "records",
        [("PK", "id"), ("FK", "actor_id")],
        [
            ("entry_id", "text, unique; the ID PENSIEVE uses to keep the entry once"),
            ("occurred_at", "time (UTC)"),
            (
                "action",
                "glossary change, memory add or removal, gate change, source order or "
                "state, key, role or setting (R-REC-02)",
            ),
            ("details", "structured text; who, what and when, never text sent by a caller"),
            ("attempts, next_try_at", "number; time (UTC); retried by PARADOX"),
            ("kept", "until PENSIEVE accepts it, at most 30 days"),
        ],
    ),
    "alert": (
        "records",
        [("PK", "id"), ("FK", "caller_id"), ("FK", "source_id")],
        [
            (
                "kind",
                "BUMBLEBEE not answering, source paused or resumed, no source for a "
                "direction, failures over 20 percent, caller or language flagged (R-ADM-03)",
            ),
            ("raised_at, sent_at", "time (UTC); sent_at empty until HEDWIG accepts it"),
            ("hedwig_notice_id", "text; returned by HEDWIG"),
            ("kept", "90 days"),
        ],
    ),
}

# Links: (child, foreign key, parent, optional, one_to_one).
LINKS = [
    ("role_grant", "person_id", "person", False, False),
    ("change_history", "changed_by", "person", False, False),
    ("audit_outbox", "actor_id", "person", True, False),
    ("caller_key", "caller_id", "caller", False, False),
    ("caller_origin", "caller_id", "caller", False, False),
    ("source_offer", "source_id", "source", False, False),
    ("source_order", "source_id", "source", False, False),
    ("gate_decision", "source_id", "source", False, False),
    ("glossary_entry", "replaces_id", "glossary_entry", True, False),
    ("glossary_entry", "glossary_import_id", "glossary_import", True, False),
    ("glossary_entry", "published_in", "glossary_version", True, False),
    ("glossary_term", "glossary_entry_id", "glossary_entry", False, False),
    ("memory_entry", "caller_id", "caller", False, False),
    ("memory_entry", "glossary_version", "glossary_version", False, False),
    ("memory_entry", "replaced_by_id", "memory_entry", True, False),
    ("cache_piece", "source_id", "source", False, False),
    ("cache_piece", "glossary_version", "glossary_version", False, False),
    ("usage_record", "caller_id", "caller", False, False),
    ("daily_total", "caller_id", "caller", False, False),
    ("alert", "caller_id", "caller", True, False),
    ("alert", "source_id", "source", True, False),
]

# Foreign keys to the hub that are not drawn (covered by the legend).
HUB_FIELDS = {"person_id", "actor_id"}


def is_hub_field(name):
    return name in HUB_FIELDS or name.endswith("_by")


def node(name):
    area, keys, _ = ENTITIES[name]
    border, fill = AREAS[area][1], AREAS[area][2]
    rows = "".join(
        f'<TR><TD ALIGN="LEFT"><FONT POINT-SIZE="10" COLOR="#64748b">{k}</FONT></TD>'
        f'<TD ALIGN="LEFT"><FONT POINT-SIZE="11">{v}</FONT></TD></TR>'
        for k, v in keys
    )
    width = 2 if name == HUB else 1
    return (
        f'  {name} [label=<<TABLE BORDER="{width}" CELLBORDER="0" CELLSPACING="0" '
        f'CELLPADDING="3" COLOR="{border}" BGCOLOR="white">'
        f'<TR><TD COLSPAN="2" BGCOLOR="{fill}"><B>{name}</B></TD></TR>{rows}</TABLE>>];'
    )


def dot_source():
    out = [
        "digraph rosetta_erd {",
        "  graph [rankdir=BT, splines=ortho, newrank=true, nodesep=0.6, ranksep=0.8,",
        '         fontname="Helvetica", pad=0.3];',
        '  node [shape=plaintext, fontname="Helvetica"];',
        '  edge [dir=both, arrowtail=crow, arrowhead=tee, color="#334155", penwidth=1.1];',
    ]
    for key, (title, border, _) in AREAS.items():
        out.append(f"  subgraph cluster_{key} {{")
        out.append(
            f'    label=<<B>{title}</B>>; style="dashed,rounded"; color="{border}";'
            f' fontcolor="{border}"; fontsize=14;'
        )
        for name, (area, _, _) in ENTITIES.items():
            if area == key:
                out.append("  " + node(name))
        out.append("  }")
    out.append(f"  {{ rank=min; {'; '.join(TOP)}; }}")
    for group in SAME_RANK:
        out.append(f"  {{ rank=same; {'; '.join(group)}; }}")
    for lower, upper in STACK:
        out.append(f"  {lower} -> {upper} [style=invis];")
    for child, _fk, parent, optional, one in LINKS:
        attrs = []
        if optional:
            attrs.append("style=dashed")
        if one:
            attrs += ["arrowtail=tee", 'xlabel=<<FONT POINT-SIZE="9">1 to 1</FONT>>']
        out.append(f"  {child} -> {parent} [{', '.join(attrs)}];")
    out.append("}")
    return "\n".join(out) + "\n"


def dictionary():
    parts = []
    for i, (key, (title, _, _)) in enumerate(AREAS.items(), start=1):
        parts.append(f"#### A.2.{i} {title}\n\n| Entity | Fields |\n|---|---|")
        for name, (area, _keys, fields) in ENTITIES.items():
            if area != key:
                continue
            text = "; ".join(f"`{f}`: {d}" for f, d in fields) if fields else "keys only"
            parts.append(f"| `{name}` | {text} |")
        parts.append("")
    return "\n".join(parts).rstrip() + "\n"


def check():
    fks = {(c, fk) for c, fk, *_ in LINKS}
    for name, (_, keys, _) in ENTITIES.items():
        for kind, k in keys:
            if "FK" in kind and (name, k) not in fks and not is_hub_field(k):
                raise SystemExit(f"{name}.{k} has no link")
    for c, _, p, *_ in LINKS:
        if c not in ENTITIES or p not in ENTITIES:
            raise SystemExit(f"link {c} -> {p} names an unknown entity")


def main():
    check()
    dot_path = OUT / f"{NAME}.dot"
    dot_path.write_text(dot_source())
    subprocess.run(["dot", "-Tsvg", str(dot_path), "-o", str(OUT / f"{NAME}.svg")], check=True)
    text = PRD.read_text()
    block = "<!-- dictionary:start -->\n" + dictionary() + "<!-- dictionary:end -->"
    text, n = re.subn(
        r"<!-- dictionary:start -->.*?<!-- dictionary:end -->",
        lambda _: block,
        text,
        flags=re.S,
    )
    if n != 1:
        raise SystemExit("PRD needs exactly one dictionary marker pair")
    PRD.write_text(text)
    print(f"{len(ENTITIES)} entities, {len(LINKS)} links")


if __name__ == "__main__":
    main()
