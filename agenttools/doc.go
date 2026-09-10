// Package agenttools exposes the Dozor Classic engine to an LLM agent as a
// catalog of typed tools.
//
// The catalog is the single source of truth for every agent surface: the CLI,
// the MCP server, and anything embedded in a mobile app. Tools never write to
// stdout, never panic to report a failure, and never mutate package-level
// state, so the same catalog can serve several agents at once.
//
// Access is governed by a Policy. Read tools are always available; mutating
// ones are hidden under PolicyReadonly, routed through a Confirmer under
// PolicyApprove, and allowed outright under PolicyFull.
package agenttools
