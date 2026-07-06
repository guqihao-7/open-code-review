package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	internalmcp "github.com/open-code-review/open-code-review/internal/mcp"
)

const (
	defaultGitHubMCPServerName = "github"
	gitHubPRContextMaxChars    = 16000
	gitHubIssueContextMaxRefs  = 5
)

type githubMCPClient interface {
	Name() string
	Tools() []*mcpsdk.Tool
	CallTool(ctx context.Context, name string, args map[string]any) (string, error)
}

type githubPRTarget struct {
	Owner  string
	Repo   string
	Number int
}

func maybeAppendGitHubPRContext(ctx context.Context, background string, clients []*internalmcp.Client, repoDir string) string {
	if !githubPRContextEnabled() {
		return background
	}

	callers := make([]githubMCPClient, 0, len(clients))
	for _, c := range clients {
		callers = append(callers, c)
	}

	client := selectGitHubMCPClient(callers, os.Getenv("OCR_GITHUB_MCP_SERVER"))
	if client == nil {
		return background
	}
	if !mcpClientHasTool(client, "pull_request_read") {
		fmt.Fprintf(os.Stderr, "[ocr] WARNING: GitHub MCP server %q does not expose pull_request_read; skipping PR context\n", client.Name())
		return background
	}

	target, ok := detectGitHubPRTarget(repoDir)
	if !ok {
		return background
	}

	contextText, err := collectGitHubPRContext(ctx, client, target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ocr] WARNING: failed to collect GitHub PR context via MCP server %q: %v\n", client.Name(), err)
		return background
	}
	if contextText == "" {
		return background
	}
	return appendBackgroundSection(background, contextText)
}

func githubPRContextEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("OCR_GITHUB_PR_CONTEXT")))
	return v != "0" && v != "false" && v != "off" && v != "no"
}

func selectGitHubMCPClient(clients []githubMCPClient, preferred string) githubMCPClient {
	preferred = strings.TrimSpace(preferred)
	if preferred != "" {
		for _, c := range clients {
			if c.Name() == preferred {
				return c
			}
		}
		return nil
	}

	for _, c := range clients {
		if c.Name() == defaultGitHubMCPServerName {
			return c
		}
	}
	for _, c := range clients {
		if mcpClientHasTool(c, "pull_request_read") {
			return c
		}
	}
	return nil
}

func collectGitHubPRContext(ctx context.Context, client githubMCPClient, target githubPRTarget) (string, error) {
	prArgs := map[string]any{
		"owner":      target.Owner,
		"repo":       target.Repo,
		"pullNumber": target.Number,
		"method":     "get",
	}
	prText, err := client.CallTool(ctx, "pull_request_read", prArgs)
	if err != nil {
		return "", err
	}

	var issueSections []string
	if mcpClientHasTool(client, "issue_read") {
		for _, issueNumber := range extractLinkedIssueNumbers(prText, target.Owner, target.Repo, gitHubIssueContextMaxRefs) {
			issueText, issueErr := client.CallTool(ctx, "issue_read", map[string]any{
				"owner":        target.Owner,
				"repo":         target.Repo,
				"issue_number": issueNumber,
				"method":       "get",
			})
			if issueErr != nil {
				fmt.Fprintf(os.Stderr, "[ocr] WARNING: failed to collect GitHub issue #%d via MCP: %v\n", issueNumber, issueErr)
				continue
			}
			issueSections = append(issueSections, fmt.Sprintf("Issue #%d:\n%s", issueNumber, issueText))
		}
	}

	var sb strings.Builder
	sb.WriteString("### GitHub Pull Request Context (from GitHub MCP)\n")
	sb.WriteString(fmt.Sprintf("Repository: %s/%s\n", target.Owner, target.Repo))
	sb.WriteString(fmt.Sprintf("Pull Request: #%d\n\n", target.Number))
	sb.WriteString("Pull Request Details:\n")
	sb.WriteString(prText)
	if len(issueSections) > 0 {
		sb.WriteString("\n\nLinked Issues:\n")
		sb.WriteString(strings.Join(issueSections, "\n\n"))
	}
	return truncateForPrompt(sb.String(), gitHubPRContextMaxChars), nil
}

func detectGitHubPRTarget(repoDir string) (githubPRTarget, bool) {
	owner, repo, ok := detectGitHubRepo(repoDir)
	if !ok {
		return githubPRTarget{}, false
	}
	number, ok := detectGitHubPRNumber()
	if !ok || number <= 0 {
		return githubPRTarget{}, false
	}
	return githubPRTarget{Owner: owner, Repo: repo, Number: number}, true
}

func detectGitHubRepo(repoDir string) (string, string, bool) {
	for _, envKey := range []string{"OCR_GITHUB_REPOSITORY", "GITHUB_REPOSITORY"} {
		if owner, repo, ok := splitGitHubRepository(os.Getenv(envKey)); ok {
			return owner, repo, true
		}
	}
	if repoDir != "" {
		if out, err := runGitCmd(repoDir, "remote", "get-url", "origin"); err == nil {
			if owner, repo, ok := parseGitHubRemoteURL(strings.TrimSpace(string(out))); ok {
				return owner, repo, true
			}
		}
	}
	return "", "", false
}

func detectGitHubPRNumber() (int, bool) {
	for _, envKey := range []string{"OCR_GITHUB_PR_NUMBER", "GITHUB_EVENT_NUMBER"} {
		if n, ok := parsePositiveInt(os.Getenv(envKey)); ok {
			return n, true
		}
	}

	if n, ok := parsePullRef(os.Getenv("GITHUB_REF")); ok {
		return n, true
	}

	eventPath := strings.TrimSpace(os.Getenv("GITHUB_EVENT_PATH"))
	if eventPath == "" {
		return 0, false
	}
	data, err := os.ReadFile(eventPath)
	if err != nil {
		return 0, false
	}
	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		return 0, false
	}
	if n, ok := numberFromJSON(event["number"]); ok {
		return n, true
	}
	if pr, ok := event["pull_request"].(map[string]any); ok {
		if n, ok := numberFromJSON(pr["number"]); ok {
			return n, true
		}
	}
	return 0, false
}

func splitGitHubRepository(raw string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(raw), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), true
}

func parseGitHubRemoteURL(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}

	if strings.HasPrefix(raw, "git@github.com:") {
		return splitGitHubRepository(strings.TrimPrefix(raw, "git@github.com:"))
	}
	if strings.HasPrefix(raw, "ssh://git@github.com/") {
		return splitGitHubRepository(strings.TrimPrefix(raw, "ssh://git@github.com/"))
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host != "github.com" {
		return "", "", false
	}
	return splitGitHubRepository(strings.TrimPrefix(u.Path, "/"))
}

func parsePullRef(raw string) (int, bool) {
	m := regexp.MustCompile(`^refs/pull/([0-9]+)/`).FindStringSubmatch(strings.TrimSpace(raw))
	if len(m) != 2 {
		return 0, false
	}
	return parsePositiveInt(m[1])
}

func parsePositiveInt(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func numberFromJSON(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n <= 0 || n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	case string:
		return parsePositiveInt(n)
	default:
		return 0, false
	}
}

func mcpClientHasTool(client githubMCPClient, name string) bool {
	for _, t := range client.Tools() {
		if t != nil && t.Name == name {
			return true
		}
	}
	return false
}

func extractLinkedIssueNumbers(text, owner, repo string, limit int) []int {
	if limit <= 0 {
		return nil
	}
	seen := map[int]struct{}{}
	var out []int
	add := func(raw string) {
		if len(out) >= limit {
			return
		}
		n, ok := parsePositiveInt(raw)
		if !ok {
			return
		}
		if _, exists := seen[n]; exists {
			return
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}

	escapedOwner := regexp.QuoteMeta(owner)
	escapedRepo := regexp.QuoteMeta(repo)
	urlRe := regexp.MustCompile(`(?i)github\.com/` + escapedOwner + `/` + escapedRepo + `/issues/([0-9]+)`)
	for _, m := range urlRe.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}

	keywordRe := regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?|refs?|references?)\s+(?:[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)?#([0-9]+)`)
	for _, m := range keywordRe.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}

	return out
}

func appendBackgroundSection(background, section string) string {
	background = strings.TrimSpace(background)
	section = strings.TrimSpace(section)
	if background == "" {
		return section
	}
	if section == "" {
		return background
	}
	return background + "\n\n" + section
}

func truncateForPrompt(s string, maxChars int) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	const marker = "\n\n[truncated]"
	if maxChars <= len(marker) {
		return s[:maxChars]
	}
	return s[:maxChars-len(marker)] + marker
}
