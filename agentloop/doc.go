// Package agentloop runs an LLM agent over the engine toolset.
//
// The loop itself belongs to PicoClaw: it owns the provider call, the
// conversation, the tool-call iteration and the iteration limit. This package
// binds an [agenttools.Catalog] to that loop, watches it, and reports what
// happened through [Callbacks] so a CLI, a TUI or a test can render the run
// however it likes.
//
// Authorization is not repeated here. A mutating tool is gated by the
// catalog's own Policy and Confirmer, so a surface that wants to ask the user
// installs a Confirmer on the catalog rather than intercepting calls.
package agentloop
