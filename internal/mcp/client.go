package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/net/http/httpguts"
)

// ClientConfig describes how to connect to a single MCP server.
type ClientConfig struct {
	Name                 string
	Transport            string
	Command              string
	Args                 []string
	Env                  []string
	URL                  string
	Headers              []string
	Dir                  string
	Version              string
	DisableStandaloneSSE bool
}

// Client wraps a single MCP server connection.
type Client struct {
	name    string
	session *mcpsdk.ClientSession
	tools   []*mcpsdk.Tool
}

// NewClient starts an MCP server subprocess, initializes the connection,
// and caches the list of available tools. The context governs the
// initialization timeout (Connect + ListTools), NOT the subprocess
// lifetime — the subprocess stays alive until Close is called.
// When dir is non-empty, the subprocess runs with that working directory.
func NewClient(ctx context.Context, name, command string, args, env []string, dir, version string) (*Client, error) {
	return NewClientWithConfig(ctx, ClientConfig{
		Name:      name,
		Transport: "stdio",
		Command:   command,
		Args:      args,
		Env:       env,
		Dir:       dir,
		Version:   version,
	})
}

// NewClientWithConfig initializes an MCP client using stdio, streamable HTTP, or legacy SSE.
func NewClientWithConfig(ctx context.Context, cfg ClientConfig) (*Client, error) {
	transport, err := newTransport(cfg)
	if err != nil {
		return nil, fmt.Errorf("configure MCP server %q: %w", cfg.Name, err)
	}

	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "open-code-review", Version: cfg.Version},
		nil,
	)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to MCP server %q: %w", cfg.Name, err)
	}

	var success bool
	defer func() {
		if !success {
			session.Close()
		}
	}()

	toolsResult, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list tools from MCP server %q: %w", cfg.Name, err)
	}

	success = true
	return &Client{
		name:    cfg.Name,
		session: session,
		tools:   toolsResult.Tools,
	}, nil
}

func newTransport(cfg ClientConfig) (mcpsdk.Transport, error) {
	switch NormalizeTransport(cfg.Transport) {
	case "stdio":
		if cfg.Command == "" {
			return nil, fmt.Errorf("command is required for stdio transport")
		}
		cmd := exec.Command(cfg.Command, cfg.Args...)
		cmd.Env = append(os.Environ(), cfg.Env...)
		if cfg.Dir != "" {
			cmd.Dir = cfg.Dir
		}
		return &mcpsdk.CommandTransport{Command: cmd}, nil
	case "http":
		if cfg.URL == "" {
			return nil, fmt.Errorf("url is required for http transport")
		}
		httpClient, err := httpClientWithHeaders(cfg.Headers)
		if err != nil {
			return nil, err
		}
		return &mcpsdk.StreamableClientTransport{
			Endpoint:             cfg.URL,
			HTTPClient:           httpClient,
			DisableStandaloneSSE: cfg.DisableStandaloneSSE,
		}, nil
	case "sse":
		if cfg.URL == "" {
			return nil, fmt.Errorf("url is required for sse transport")
		}
		httpClient, err := httpClientWithHeaders(cfg.Headers)
		if err != nil {
			return nil, err
		}
		return &mcpsdk.SSEClientTransport{
			Endpoint:   cfg.URL,
			HTTPClient: httpClient,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported transport %q (supported: stdio, http, sse)", cfg.Transport)
	}
}

func NormalizeTransport(transport string) string {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "", "stdio":
		return "stdio"
	case "http", "streamable", "streamable_http", "streamable-http":
		return "http"
	case "sse":
		return "sse"
	default:
		return ""
	}
}

func httpClientWithHeaders(entries []string) (*http.Client, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	headers := make(http.Header, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid header entry %q: must be in Header=Value format", entry)
		}
		if !httpguts.ValidHeaderFieldName(key) {
			return nil, fmt.Errorf("invalid header name %q", key)
		}
		// Values may contain auth material; expand env vars only at request setup and never log them.
		headers.Add(key, os.ExpandEnv(value))
	}
	return &http.Client{
		Transport: headerRoundTripper{
			base:    http.DefaultTransport,
			headers: headers,
		},
	}, nil
}

type headerRoundTripper struct {
	base    http.RoundTripper
	headers http.Header
}

func (rt headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	next := req.Clone(req.Context())
	next.Header = req.Header.Clone()
	for key, values := range rt.headers {
		for _, value := range values {
			next.Header.Add(key, value)
		}
	}
	return base.RoundTrip(next)
}

func (c *Client) Name() string          { return c.name }
func (c *Client) Tools() []*mcpsdk.Tool { return c.tools }

// CallTool invokes a tool on the MCP server and returns the text result.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	params := &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: args,
	}

	result, err := c.session.CallTool(ctx, params)
	if err != nil {
		return "", fmt.Errorf("call MCP tool %q: %w", name, err)
	}

	if result.IsError {
		return fmt.Sprintf("MCP tool %q returned an error: %s", name, contentToText(result.Content)), nil
	}

	return contentToText(result.Content), nil
}

func (c *Client) Close() error {
	return c.session.Close()
}

func contentToText(contents []mcpsdk.Content) string {
	var parts []string
	for _, item := range contents {
		switch v := item.(type) {
		case *mcpsdk.TextContent:
			parts = append(parts, v.Text)
		default:
			parts = append(parts, fmt.Sprintf("[unsupported content type: %T]", item))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}
