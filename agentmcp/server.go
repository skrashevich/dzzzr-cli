// Package agentmcp serves the Dozor Classic toolset over the Model Context
// Protocol, so any MCP client can read the game and, when allowed, play it
// through the same tools the CLI and the mobile agent use.
package agentmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/skrashevich/dzzzr-cli/agenttools"
)

// ServerName is the implementation name advertised to MCP clients.
const ServerName = "dzzzr-engine"

// NewServer publishes a catalog as an MCP server.
//
// Under agenttools.PolicyApprove every mutating call is put to the client
// first: the handler answers with an input request, the client asks the user,
// and the call is retried with the answer. Clients that cannot ask get a
// refusal rather than a silent mutation.
//
// The catalog must be built with a Confirmer for PolicyApprove, but the
// server never uses it: authorization is decided here, from the client's
// answer, and handed to the catalog per call.
func NewServer(catalog *agenttools.Catalog, version string) (*mcp.Server, error) {
	if catalog == nil {
		return nil, errors.New("agentmcp: catalog is required")
	}
	if version == "" {
		version = "dev"
	}
	server := mcp.NewServer(&mcp.Implementation{Name: ServerName, Version: version}, &mcp.ServerOptions{
		Instructions: catalog.SystemPromptAddendum(),
	})
	for _, tool := range catalog.Tools() {
		def := &mcp.Tool{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: tool.Parameters(),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: !tool.Mutating() && !tool.WritesLocalFiles()},
		}
		server.AddTool(def, toolHandler(catalog, tool))
	}
	return server, nil
}

// ServeStdio runs the server over stdin and stdout until the client
// disconnects or ctx is canceled.
func ServeStdio(ctx context.Context, server *mcp.Server) error {
	return server.Run(ctx, &mcp.StdioTransport{})
}

func toolHandler(catalog *agenttools.Catalog, tool *agenttools.Tool) mcp.ToolHandler {
	needsApproval := tool.Mutating() && catalog.Policy() == agenttools.PolicyApprove
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := map[string]any{}
		if req.Params != nil && len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return errorResult(fmt.Sprintf("%s: arguments are not a JSON object: %v", tool.Name(), err)), nil
			}
		}
		if needsApproval {
			answered, confirmed := answerFromRequest(req, args)
			if !answered {
				// Ask the client and let it retry the call with the answer.
				return &mcp.CallToolResult{InputRequests: confirmationRequest(tool.Name(), args)}, nil
			}
			if !confirmed {
				return errorResult(fmt.Sprintf("%s was not authorized for these arguments. Do not retry it; ask what to do instead.", tool.Name())), nil
			}
			ctx = agenttools.WithConfirmer(ctx, decidedConfirmer{allow: true})
		}
		result := tool.Execute(ctx, args)
		return &mcp.CallToolResult{
			IsError: result.IsError,
			Content: []mcp.Content{&mcp.TextContent{Text: result.Content}},
		}, nil
	}
}

func errorResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}
