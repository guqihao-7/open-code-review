package mcp

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const runAsServerEnv = "_OCR_MCP_TEST_SERVER"

func TestMain(m *testing.M) {
	if os.Getenv(runAsServerEnv) != "" {
		runTestMCPServer()
		return
	}
	os.Exit(m.Run())
}

func runTestMCPServer() {
	server := newTestMCPServer()
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

func newTestMCPServer() *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "test-server", Version: "v0.0.1"},
		nil,
	)
	server.AddTool(
		&mcp.Tool{
			Name:        "echo",
			Description: "Echoes input",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{"type": "string"},
				},
			},
		},
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return nil, err
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "echo: " + args.Message}},
			}, nil
		},
	)
	return server
}

func TestNewClient_Stdio(t *testing.T) {
	requireExec(t)

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	c, err := NewClient(ctx, "test-srv", exe, nil, []string{runAsServerEnv + "=1"}, "", "v0.0.1")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	if c.Name() != "test-srv" {
		t.Errorf("Name() = %q, want %q", c.Name(), "test-srv")
	}

	found := false
	for _, tool := range c.Tools() {
		if tool.Name == "echo" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected echo tool in Tools()")
	}

	result, err := c.CallTool(ctx, "echo", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result != "echo: hello" {
		t.Errorf("CallTool result = %q, want %q", result, "echo: hello")
	}

	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNewClient_Stdio_BadCommand(t *testing.T) {
	requireExec(t)

	ctx := context.Background()
	_, err := NewClient(ctx, "bad", "/nonexistent/mcp-server-binary", nil, nil, "", "v0.0.1")
	if err == nil {
		t.Fatal("expected error for bad command, got nil")
	}
}

func TestNewClient_StreamableHTTP(t *testing.T) {
	t.Setenv("OCR_TEST_MCP_TOKEN", "secret")

	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		if got := req.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer secret")
		}
		return newTestMCPServer()
	}, nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx := context.Background()
	c, err := NewClientWithConfig(ctx, ClientConfig{
		Name:                 "http-srv",
		Transport:            "http",
		URL:                  server.URL,
		Headers:              []string{"Authorization=Bearer ${OCR_TEST_MCP_TOKEN}"},
		Version:              "v0.0.1",
		DisableStandaloneSSE: true,
	})
	if err != nil {
		t.Fatalf("NewClientWithConfig: %v", err)
	}
	defer c.Close()

	found := false
	for _, tool := range c.Tools() {
		if tool.Name == "echo" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected echo tool in Tools()")
	}

	result, err := c.CallTool(ctx, "echo", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result != "echo: hello" {
		t.Errorf("CallTool result = %q, want %q", result, "echo: hello")
	}
}

func TestNewClientWithConfig_MissingHTTPURL(t *testing.T) {
	_, err := NewClientWithConfig(context.Background(), ClientConfig{
		Name:      "missing-url",
		Transport: "http",
	})
	if err == nil {
		t.Fatal("expected error for missing HTTP URL")
	}
}

func requireExec(t *testing.T) {
	t.Helper()
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
	default:
		t.Skip("unsupported OS for subprocess test")
	}
}
