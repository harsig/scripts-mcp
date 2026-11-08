package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect starts a server for scripts and returns a connected client session.
func connect(t *testing.T, shutdown context.Context, scripts []Script) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	cfg := &Config{TimeoutSeconds: 5, MaxOutputBytes: 1000}
	if _, err := NewServer(shutdown, cfg, scripts).Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func TestServerListTools(t *testing.T) {
	session := connect(t, context.Background(), []Script{
		{Name: "alpha", Description: "A script", Params: map[string]Param{"n": {Type: "integer", Required: true}}},
		{Name: "beta"},
	})
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(res.Tools))
	}
	alpha, beta := res.Tools[0], res.Tools[1]
	if alpha.Name != "alpha" || alpha.Description != "A script" || beta.Name != "beta" || beta.Description != "" {
		t.Errorf("tools = %+v, %+v", alpha, beta)
	}
	schema, ok := alpha.InputSchema.(map[string]any)
	if !ok || schema["additionalProperties"] != false || len(schema["required"].([]any)) != 1 {
		t.Errorf("alpha schema = %#v", alpha.InputSchema)
	}
}

func TestServerCallTool(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "greet.sh")
	writeFile(t, file, "#!/bin/sh\n[ \"$PARAM_NAME\" != quiet ] || exit 0\necho \"hello $PARAM_NAME\"\necho log >&2\n[ \"$PARAM_NAME\" != fail ] || { echo oops >&2; exit 2; }\n", 0o755)
	session := connect(t, context.Background(), []Script{
		{Name: "greet", File: file, Params: map[string]Param{"name": {Type: "string", Required: true}}},
		{Name: "missing", File: filepath.Join(dir, "missing.sh")},
	})

	tests := []struct {
		name      string
		tool      string
		args      map[string]any
		wantText  []string
		wantError bool
	}{
		{name: "success", tool: "greet", args: map[string]any{"name": "world"}, wantText: []string{"hello world\n"}},
		{name: "no output", tool: "greet", args: map[string]any{"name": "quiet"}, wantText: []string{"(no output)"}},
		{name: "invalid arguments", tool: "greet", args: map[string]any{}, wantText: []string{"invalid arguments", `missing required argument "name"`}, wantError: true},
		{name: "unknown argument", tool: "greet", args: map[string]any{"name": "x", "x": 1}, wantText: []string{`unknown argument "x"`}, wantError: true},
		{name: "exit code", tool: "greet", args: map[string]any{"name": "fail"}, wantText: []string{"exit code 2", "stderr:\nlog\noops", "stdout:\nhello fail"}, wantError: true},
		{name: "start failure", tool: "missing", wantText: []string{"script missing: failed"}, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tt.tool, Arguments: tt.args})
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError != tt.wantError {
				t.Fatalf("IsError = %v, want %v (%v)", res.IsError, tt.wantError, res.Content)
			}
			if len(res.Content) != 1 {
				t.Fatalf("got %d content blocks, want 1", len(res.Content))
			}
			text := res.Content[0].(*mcp.TextContent).Text
			for _, want := range tt.wantText {
				if !strings.Contains(text, want) {
					t.Errorf("text = %q, want containing %q", text, want)
				}
			}
		})
	}
}

func TestServerShutdownKillsScripts(t *testing.T) {
	file := filepath.Join(t.TempDir(), "slow.sh")
	writeFile(t, file, "#!/bin/sh\nsleep 10\n", 0o755)
	shutdown, stop := context.WithCancel(context.Background())
	session := connect(t, shutdown, []Script{{Name: "slow", File: file}})

	time.AfterFunc(100*time.Millisecond, stop)
	start := time.Now()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "slow"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || time.Since(start) > 3*time.Second {
		t.Errorf("IsError = %v after %s, want a quick tool error", res.IsError, time.Since(start))
	}
}
