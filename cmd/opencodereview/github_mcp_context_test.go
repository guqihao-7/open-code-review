package main

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeGitHubMCPClient struct {
	name  string
	tools []*mcpsdk.Tool
	calls []fakeGitHubMCPCall
}

type fakeGitHubMCPCall struct {
	name string
	args map[string]any
}

func (f *fakeGitHubMCPClient) Name() string {
	return f.name
}

func (f *fakeGitHubMCPClient) Tools() []*mcpsdk.Tool {
	return f.tools
}

func (f *fakeGitHubMCPClient) CallTool(_ context.Context, name string, args map[string]any) (string, error) {
	f.calls = append(f.calls, fakeGitHubMCPCall{name: name, args: args})
	switch name {
	case "pull_request_read":
		return "title: fix cache\nbody: Fixes #123 and references https://github.com/alibaba/open-code-review/issues/456", nil
	case "issue_read":
		return fmt.Sprintf("issue body for #%v", args["issue_number"]), nil
	default:
		return "", fmt.Errorf("unexpected tool %s", name)
	}
}

func TestParseGitHubRemoteURL(t *testing.T) {
	tests := []struct {
		raw   string
		owner string
		repo  string
		ok    bool
	}{
		{"git@github.com:alibaba/open-code-review.git", "alibaba", "open-code-review", true},
		{"https://github.com/alibaba/open-code-review.git", "alibaba", "open-code-review", true},
		{"ssh://git@github.com/alibaba/open-code-review.git", "alibaba", "open-code-review", true},
		{"https://example.com/alibaba/open-code-review.git", "", "", false},
	}

	for _, tt := range tests {
		owner, repo, ok := parseGitHubRemoteURL(tt.raw)
		if owner != tt.owner || repo != tt.repo || ok != tt.ok {
			t.Errorf("parseGitHubRemoteURL(%q) = (%q, %q, %v), want (%q, %q, %v)", tt.raw, owner, repo, ok, tt.owner, tt.repo, tt.ok)
		}
	}
}

func TestDetectGitHubPRNumberFromEnv(t *testing.T) {
	t.Setenv("OCR_GITHUB_PR_NUMBER", "42")
	t.Setenv("GITHUB_EVENT_NUMBER", "")
	t.Setenv("GITHUB_REF", "")
	t.Setenv("GITHUB_EVENT_PATH", "")

	got, ok := detectGitHubPRNumber()
	if !ok || got != 42 {
		t.Fatalf("detectGitHubPRNumber() = (%d, %v), want (42, true)", got, ok)
	}
}

func TestDetectGitHubPRNumberFromPullRef(t *testing.T) {
	t.Setenv("OCR_GITHUB_PR_NUMBER", "")
	t.Setenv("GITHUB_EVENT_NUMBER", "")
	t.Setenv("GITHUB_REF", "refs/pull/77/merge")
	t.Setenv("GITHUB_EVENT_PATH", "")

	got, ok := detectGitHubPRNumber()
	if !ok || got != 77 {
		t.Fatalf("detectGitHubPRNumber() = (%d, %v), want (77, true)", got, ok)
	}
}

func TestDetectGitHubPRNumberFromEventPath(t *testing.T) {
	eventPath := t.TempDir() + "/event.json"
	if err := writeTestFile(eventPath, `{"pull_request":{"number":88}}`); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OCR_GITHUB_PR_NUMBER", "")
	t.Setenv("GITHUB_EVENT_NUMBER", "")
	t.Setenv("GITHUB_REF", "")
	t.Setenv("GITHUB_EVENT_PATH", eventPath)

	got, ok := detectGitHubPRNumber()
	if !ok || got != 88 {
		t.Fatalf("detectGitHubPRNumber() = (%d, %v), want (88, true)", got, ok)
	}
}

func TestExtractLinkedIssueNumbers(t *testing.T) {
	text := "Fixes #123, references alibaba/open-code-review#234, see https://github.com/alibaba/open-code-review/issues/456 and duplicate #123"

	got := extractLinkedIssueNumbers(text, "alibaba", "open-code-review", 5)
	want := []int{456, 123, 234}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractLinkedIssueNumbers() = %v, want %v", got, want)
	}
}

func TestSelectGitHubMCPClient(t *testing.T) {
	generic := &fakeGitHubMCPClient{name: "other", tools: []*mcpsdk.Tool{{Name: "pull_request_read"}}}
	named := &fakeGitHubMCPClient{name: "github", tools: []*mcpsdk.Tool{{Name: "different"}}}

	if got := selectGitHubMCPClient([]githubMCPClient{generic, named}, ""); got != named {
		t.Fatal("expected client named github to win over tool-based fallback")
	}
	if got := selectGitHubMCPClient([]githubMCPClient{generic, named}, "other"); got != generic {
		t.Fatal("expected preferred client to win")
	}
}

func TestCollectGitHubPRContext(t *testing.T) {
	client := &fakeGitHubMCPClient{
		name: "github",
		tools: []*mcpsdk.Tool{
			{Name: "pull_request_read"},
			{Name: "issue_read"},
		},
	}

	got, err := collectGitHubPRContext(context.Background(), client, githubPRTarget{
		Owner:  "alibaba",
		Repo:   "open-code-review",
		Number: 12,
	})
	if err != nil {
		t.Fatalf("collectGitHubPRContext() error = %v", err)
	}
	if len(client.calls) != 3 {
		t.Fatalf("tool calls = %d, want 3", len(client.calls))
	}
	if client.calls[0].name != "pull_request_read" || client.calls[0].args["pullNumber"] != 12 {
		t.Fatalf("first call = %+v", client.calls[0])
	}
	if client.calls[1].name != "issue_read" || client.calls[1].args["issue_number"] != 456 {
		t.Fatalf("second call = %+v", client.calls[1])
	}
	if client.calls[2].name != "issue_read" || client.calls[2].args["issue_number"] != 123 {
		t.Fatalf("third call = %+v", client.calls[2])
	}
	for _, want := range []string{
		"GitHub Pull Request Context",
		"Repository: alibaba/open-code-review",
		"Pull Request: #12",
		"Issue #456",
		"Issue #123",
	} {
		if !contains(got, want) {
			t.Fatalf("context missing %q:\n%s", want, got)
		}
	}
}

func TestAppendBackgroundSection(t *testing.T) {
	got := appendBackgroundSection("existing", "github")
	if got != "existing\n\ngithub" {
		t.Fatalf("appendBackgroundSection() = %q", got)
	}
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
