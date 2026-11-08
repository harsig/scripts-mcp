package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer creates an MCP server with one tool per script. Running scripts
// are killed when shutdown is done.
func NewServer(shutdown context.Context, cfg *Config, scripts []Script) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "scripts-mcp", Version: version}, nil)
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	for _, s := range scripts {
		server.AddTool(&mcp.Tool{
			Name:        s.Name,
			Description: s.Description,
			InputSchema: s.InputSchema(),
		}, runScript(shutdown, s, timeout, cfg.MaxOutputBytes))
	}
	return server
}

// runScript returns a handler that validates the arguments and runs the script.
func runScript(shutdown context.Context, s Script, timeout time.Duration, maxOutput int) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		env, err := s.Env(req.Params.Arguments)
		if err != nil {
			return toolError(fmt.Sprintf("invalid arguments: %v", err)), nil
		}

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		defer context.AfterFunc(shutdown, cancel)()

		stdout, stderr, err := s.Run(ctx, env, timeout, maxOutput)
		if err != nil {
			text := fmt.Sprintf("script %s: %v", s.Name, err)
			if stderr != "" {
				text += "\n\nstderr:\n" + stderr
			}
			if stdout != "" {
				text += "\n\nstdout:\n" + stdout
			}
			return toolError(text), nil
		}
		if stderr != "" {
			fmt.Fprint(os.Stderr, stderr)
		}
		if stdout == "" {
			stdout = "(no output)" // an empty result reads to a model like a failed call
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: stdout}}}, nil
	}
}

func toolError(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}
