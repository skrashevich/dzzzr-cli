// Package agentloop runs the shared native and browser LLM agent. It owns
// retries, request deadlines, context integrity, tool validation and execution,
// cancellation, progress events and usage reports. Platform adapters provide
// the HTTP transport and tools. Authorization belongs to the tools' policy.
package agentloop
