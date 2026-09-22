package agenttools

// The browser editor draws its game and level forms from the very schemas the
// agent's admin_create_game / admin_create_level tools carry, so a field added
// for the agent shows up in the manual form and the two cannot drift apart.
// Only the two parameter objects are exported: the surrounding tool schema
// (game_id, level_id) is the agent's calling convention, not an author's form.

// GameParamsSchema returns the JSON Schema of the "params" object of
// admin_create_game and admin_update_game — the fields the agent may set on a
// game, with the descriptions it reads.
//
// The result is freshly built on every call: gameParamsSchema allocates the
// object, and strProp/intProp/boolProp allocate every property inside it, so
// the caller owns the whole tree and cannot reach package state through it. No
// copy is needed here.
func GameParamsSchema() map[string]any { return gameParamsSchema() }

// LevelParamsSchema returns the JSON Schema of the "params" object of
// admin_create_level and admin_update_level.
//
// Freshly built on every call for the same reason as GameParamsSchema; the
// nested code, bonus, fake-code and spoiler item schemas are allocated too.
func LevelParamsSchema() map[string]any { return levelParamsSchema() }
