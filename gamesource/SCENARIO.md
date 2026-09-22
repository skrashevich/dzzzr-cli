# Complete scenario format, version 1

`scenario.schema.json` defines a JSON Schema Draft 2020-12 document for a full
game scenario. It covers all currently modeled game settings, ordered levels,
their content and assets. `ExportScenario`, `DecodeScenario`, `EncodeScenario`,
`ValidateScenario` and `ImportScenario` implement this format. The older `Plan`
and `admin-upload-source` retain their separate level-batch format.

## CLI and LLM tools

```sh
dzzzr admin-export-scenario 1383 scenario.json
dzzzr admin-export-scenario 1383 linked-scenario.json linked
dzzzr admin-validate-scenario scenario.json
dzzzr admin-import-scenario scenario.json
dzzzr admin-import-scenario scenario.json 1500
dzzzr admin-import-scenario scenario.json 1500 level-map.json
```

Export embeds files by default. The optional positional `linked` argument keeps
download URLs instead; `-` as the output filename writes the document to stdout.
Output files are private (0600) and must not already exist. Import without a
destination creates a new game. An existing empty game accepts the scenario
directly. An existing nonempty game requires a complete mapping and exactly the
same number of levels. No extra levels are implicitly removed.

`level-map.json` is an object such as `{"level_31049": 40001, "level_31050": 40002}`:
keys come from the scenario, values identify destination levels. Source IDs are
never used as an implicit destination. Level order follows the scenario array.
The import result includes `game_id`, `level_ids`, `uploaded_assets`,
`completed_levels`, `stage`, `failed_key` and `complete`, even on failure.

LLM tools are `admin_export_scenario`, `admin_validate_scenario` and
`admin_import_scenario`. Export takes `game_id`, `path`, and optional
`linked_assets`. Import takes `path`, optional positive `game_id`, and optional
`level_ids` mapping. These are registered in the common agent/MCP catalog when
admin tools and credentials are enabled. Local paths stay inside
`DZZZR_FILES_ROOT`; export writes a file and returns its path/counts rather than
Base64 through the model context. Import follows the usual mutation policy.

The root contains `format: "dzzzr-scenario"`, `version: 1`, `game.params`,
`levels` and `assets`. Optional `source` records the original engine and game;
optional `exported_at` records an RFC 3339 timestamp. `levels` may be empty.
Every level contains a stable portable `key` and complete `params`.

## Snapshot semantics

- All modeled parameter fields are required, even when empty, zero or false.
  Missing data is an invalid snapshot, not an instruction to preserve an old
  destination value. Do not serialize existing `omitempty` structs directly:
  a dedicated export adapter must materialize the complete values.
- Empty strings clear text; empty arrays clear collections; false disables a
  flag; zero is a concrete value. Level `penalty: null` explicitly restores
  inheritance from the game. The import adapter must implement these semantics
  instead of forwarding the document to the existing patch API unchanged.
- Numeric ranges follow the engine rather than intuition. `bonus_after` is `-1`
  or greater: `-1` is the engine's own default and means the next level is
  handed over as soon as the main codes are in. The schema demanded a
  non-negative number there until recently, so exporting a game whose organizer
  never changed that field failed before any file was produced — through
  `dzzzr admin-export-scenario` and through the browser editor alike. It is
  fixed. `-2` is still rejected: `-1` is a value, not an open range.
  `TestScenarioAcceptsEngineBonusAfter` pins both halves.
- `clear` is excluded because it is an edit operation, not scenario content.
  Level `greeting` is excluded because the observed level form has no such field.
- The `levels` array defines order. `source_id` and `source_order` are optional
  provenance; they do not select a destination object. Updating an existing game
  requires an explicit destination and level mapping outside this document.
- Field spellings follow the existing API, including `anons`, `breaf_place`,
  `skvoz`, and `master_code_shtraf`. Dates use DD.MM.YYYY and times use HH:MM in
  the target engine's local time. No implicit timezone conversion is defined.
- An unset date is representable for export, but creating a game still requires
  valid nonempty name/date/time according to engine rules.
- Two hint texts and three timing fields reflect the actual current model.
  Main, bonus and fake codes retain their synonyms, difficulty, sector or time
  adjustment. Spoilers retain text, unlocking code, synonyms and penalty.
- `extensions` can preserve namespaced data that is not modeled yet, including
  archived engine fields. It is not automatically submitted during import.
  Teams, applications, messages and progress are operational data outside the
  scenario. Credentials and session cookies have no fields in this format.

## Images and other files

`assets` is a map, for example this fragment:

```json
{
  "entrance": {
    "filename": "entrance.jpg",
    "media_type": "image/jpeg",
    "path": "images/entrance.jpg"
  }
}
```

The corresponding HTML can contain `<img src="asset:entrance">`. Asset IDs
are case-sensitive. Each asset has exactly one payload source: `data_base64`
for a single self-contained JSON file, `path` for a directory/archive package,
or `url` for an external dependency. `original_url` only records provenance.
Optional `sha256` and `size_bytes` describe the actual decoded file bytes.

The importer uploads assets before level content and replaces exact `asset:ID` URL
references in HTML URL attributes and CSS URLs with returned engine URLs.
It must not perform arbitrary text replacement or execute embedded HTML.
References can include a fragment, for example `asset:manual#page=4`; it is
preserved per reference and is not part of the downloadable file identity.
Export scans hints, spoilers, announcements and organizer comments. It handles
resource attributes, image `srcset`, file links, inline styles and style-block
`url()` references. Plain text and script bodies are untouched.

Dependencies generated by JavaScript are not collected. Mixed `srcset` values
containing data URLs are retained verbatim. Files with CSS imports or relative
dependencies inside CSS/SVG/HTML payloads are rejected before import writes:
relocating such a file would break its content. Inline those dependencies or
use absolute URLs. The result is a scenario package, not a website archive;
remaining external dependencies can prevent a fully offline export.

Files are limited to 32 MiB each and 64 MiB total, and JSON to 96 MiB. Import
reads payloads and validates sizes/digests before any engine write. Remote
requests use a separate client without organizer credentials. Uploaded names
use SHA-256 plus extension to avoid collisions; existing files are downloaded
and verified before reuse. Local paths use `os.Root`, including symlink checks.

## Validation beyond JSON Schema

The importer must also validate unique level keys, asset reference resolution,
sector numbers against `sector_names`, code counts and synonym collisions,
calendar dates, coordinates and engine encoding/limits. A sector of zero means
no sector; otherwise sector N refers to `sector_names[N-1]`. `code_count: 0`
means all main codes. These cross-field constraints are checked by the
application, not this schema. Offline validation does not download payloads or
check their digests; import performs those checks. The decoder uses an embedded
schema and does not fetch the schema URL supplied by the document.

Base64 decoding, file digest/size validation, package path containment and
destination permissions require application checks. Structural validity does
not establish that the live engine will accept a file upload or a mutation.

Game settings and levels are written as complete snapshots. New levels are
created as unpublished shells and then fully replaced. Readback compares every
modeled field and rejects silent engine refusals. Level order is verified after
moves. Game publication/application/finished flags are held false during content
writes and restored last; level publication is restored after ordering.
Import into an active game can therefore affect its lifecycle.

There is no rollback or automatic retry of writes. Failure can leave uploaded
files, a new game, shell levels or a partially updated destination. `level_ids`
includes identified shells even if their replacement failed. Inspect the stage
and destination before resuming manually. `complete: true` is emitted only after
readback and final flags succeed.

Extensions survive decode/encode but are not stored in the engine; keep the
original JSON for those annotations. Export checks for membership/order changes
while reading, but the engine has no atomic snapshot API and concurrent text
edits cannot be excluded.
