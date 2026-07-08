package mcp

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNormalizeTransport(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", "stdio"},
		{" stdio ", "stdio"},
		{"HTTP", "http"},
		{"streamable", "http"},
		{"streamable_http", "http"},
		{"streamable-http", "http"},
		{"sse", "sse"},
		{"websocket", ""},
	}
	for _, tc := range tests {
		if got := NormalizeTransport(tc.in); got != tc.want {
			t.Errorf("NormalizeTransport(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHTTPClientWithHeaders(t *testing.T) {
	t.Setenv("OCR_MCP_TOKEN", "secret")

	client, err := httpClientWithHeaders([]string{"Authorization=Bearer ${OCR_MCP_TOKEN}", "X-Tenant=docs"})
	if err != nil {
		t.Fatalf("httpClientWithHeaders: %v", err)
	}
	rt, ok := client.Transport.(headerRoundTripper)
	if !ok {
		t.Fatalf("Transport = %T, want headerRoundTripper", client.Transport)
	}
	if got := rt.headers.Get("Authorization"); got != "Bearer secret" {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer secret")
	}
	if got := rt.headers.Get("X-Tenant"); got != "docs" {
		t.Errorf("X-Tenant header = %q, want %q", got, "docs")
	}
}

func TestHTTPClientWithHeadersEmpty(t *testing.T) {
	client, err := httpClientWithHeaders(nil)
	if err != nil {
		t.Fatalf("httpClientWithHeaders: %v", err)
	}
	if client != nil {
		t.Fatalf("client = %#v, want nil", client)
	}
}

func TestHTTPClientWithHeadersInvalid(t *testing.T) {
	tests := [][]string{
		{"NoEquals"},
		{"Bad Header=value"},
	}
	for _, entries := range tests {
		if _, err := httpClientWithHeaders(entries); err == nil {
			t.Fatalf("expected error for entries %v", entries)
		}
	}
}

func TestHeaderRoundTripperAddsConfiguredHeaders(t *testing.T) {
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer secret")
		}
		if got := req.Header.Values("X-Trace"); len(got) != 2 {
			t.Errorf("X-Trace values = %v, want 2 values", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Header:     make(http.Header),
		}, nil
	})
	rt := headerRoundTripper{
		base: base,
		headers: http.Header{
			"Authorization": []string{"Bearer secret"},
			"X-Trace":       []string{"a", "b"},
		},
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.com/mcp", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()
	if req.Header.Get("Authorization") != "" {
		t.Fatal("RoundTrip mutated the original request headers")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
