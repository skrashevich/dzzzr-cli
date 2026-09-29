// Package agentprotocol is the portable LLM contract shared by native and WASM agents.
// Protocol types remain aliases to PicoClaw's MIT-licensed protocoltypes package,
// so native providers and callers retain their existing interfaces.
package agentprotocol

import (
	"context"

	"github.com/sipeed/picoclaw/pkg/providers/protocoltypes"
)

type Message = protocoltypes.Message
type ToolCall = protocoltypes.ToolCall
type FunctionCall = protocoltypes.FunctionCall
type ToolDefinition = protocoltypes.ToolDefinition
type ToolFunctionDefinition = protocoltypes.ToolFunctionDefinition
type LLMResponse = protocoltypes.LLMResponse
type UsageInfo = protocoltypes.UsageInfo

type LLMProvider interface {
	Chat(context.Context, []Message, []ToolDefinition, string, map[string]any) (*LLMResponse, error)
	GetDefaultModel() string
}
