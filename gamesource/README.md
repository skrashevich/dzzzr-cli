# JSON game level batches

For the separate full-game export/import format, see
[SCENARIO.md](SCENARIO.md) and [scenario.schema.json](scenario.schema.json).
That versioned snapshot schema includes game settings and packaged assets.
Use `admin-export-scenario`, `admin-import-scenario`, and
`admin-validate-scenario` (or the corresponding underscore-named LLM tools).
The batch API below retains its existing format.

`Decode` accepts a single strict JSON document, checks every level and returns a
`Plan`. `Apply` repeats validation before any network call, verifies all update
IDs belong to the destination game, then writes in array order.

```json
{
  "game_id": 42,
  "mode": "create",
  "levels": [
    {
      "id": 0,
      "params": {
        "title": "Первый уровень",
        "question": "Текст задания",
        "publish": false,
        "codes": [{"code": "DR123", "danger": "1"}]
      }
    }
  ]
}
```

- `create` requires zero or omitted IDs. `update` requires unique positive IDs.
- Update is full replacement of modeled `LevelParams` fields: omitted values
  clear old strings, numbers and lists. Supply the complete intended level.
- Validation checks Windows-1251 encoding before writes, rejects negative
  numeric parameters, invalid difficulties, invalid coordinates and conflicting
  code/synonym values within a level. Synonyms use `#` separators.
- `Result.completed` records successful writes with zero-based source indexes
  and engine IDs. `failed_index` identifies an unsuccessful operation. Stop and
  inspect a failed write before retrying: the server may have accepted it even
  when its response was lost. Successful writes are not rolled back.
- No Google Docs parsing, Drive downloads, file upload, or automatic technical
  levels are performed. Those are not part of this JSON batch format.
