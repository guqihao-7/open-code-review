package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultCodeGraphBinary     = "codegraph"
	minCodeGraphVersion        = "0.9.0"
	codeGraphTimeout           = 10 * time.Second
	codeGraphMaxLimit          = 50
	codeGraphMaxDepth          = 10
	codeGraphMaxOutputLenBytes = 60000
)

// CodeGraphCommandRunner executes external commands for the CodeGraph tool.
type CodeGraphCommandRunner interface {
	Run(ctx context.Context, dir, name string, args ...string) (stdout, stderr string, err error)
}

type execCodeGraphRunner struct{}

func (execCodeGraphRunner) Run(ctx context.Context, dir, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// CodeGraphOptions controls CodeGraph discovery and execution.
type CodeGraphOptions struct {
	Binary     string
	MinVersion string
	Runner     CodeGraphCommandRunner
}

// CodeGraphAvailability reports whether the CodeGraph tool can be exposed.
type CodeGraphAvailability struct {
	Available bool
	Reason    string
}

// CodeGraphProvider exposes structural repository queries backed by CodeGraph.
type CodeGraphProvider struct {
	RepoDir string
	Binary  string
	Runner  CodeGraphCommandRunner
}

func NewCodeGraph(repoDir string) *CodeGraphProvider {
	return &CodeGraphProvider{
		RepoDir: repoDir,
		Binary:  defaultCodeGraphBinary,
		Runner:  execCodeGraphRunner{},
	}
}

func (p *CodeGraphProvider) Tool() Tool { return CodeGraph }

func (p *CodeGraphProvider) Execute(ctx context.Context, args map[string]any) (string, error) {
	if strings.TrimSpace(p.RepoDir) == "" {
		return "", errors.New("codegraph repo dir is empty")
	}

	binary := p.Binary
	if binary == "" {
		binary = defaultCodeGraphBinary
	}
	runner := p.Runner
	if runner == nil {
		runner = execCodeGraphRunner{}
	}

	cmdArgs, err := p.buildArgs(args)
	if err != nil {
		return "Error: " + err.Error(), nil
	}

	runCtx, cancel := context.WithTimeout(ctx, codeGraphTimeout)
	defer cancel()

	stdout, stderr, err := runner.Run(runCtx, p.RepoDir, binary, cmdArgs...)
	if runCtx.Err() != nil && err != nil {
		return "codegraph timed out. Try narrowing the query.", nil
	}
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = strings.TrimSpace(stdout)
		}
		if msg == "" {
			msg = err.Error()
		}
		return "Error: " + msg, nil
	}

	out := strings.TrimSpace(stdout)
	if out == "" {
		return "No CodeGraph results found", nil
	}
	if len(out) > codeGraphMaxOutputLenBytes {
		out = out[:codeGraphMaxOutputLenBytes] + "\n\nNote: CodeGraph output was truncated. Narrow the query and try again."
	}
	return out, nil
}

func (p *CodeGraphProvider) buildArgs(args map[string]any) ([]string, error) {
	action := strings.TrimSpace(getCodeGraphString(args, "action"))
	if action == "" {
		return nil, errors.New("action is required")
	}

	switch action {
	case "query":
		search := strings.TrimSpace(getCodeGraphString(args, "search"))
		if search == "" {
			return nil, errors.New("search is required for action query")
		}
		cmdArgs := []string{"query", "-p", p.RepoDir, "-j", "-l", strconv.Itoa(codeGraphLimit(args, 10)), search}
		if kind := strings.TrimSpace(getCodeGraphString(args, "kind")); kind != "" {
			cmdArgs = append(cmdArgs[:6], append([]string{"-k", kind}, cmdArgs[6:]...)...)
		}
		return cmdArgs, nil
	case "callers", "callees":
		symbol := strings.TrimSpace(getCodeGraphString(args, "symbol"))
		if symbol == "" {
			return nil, fmt.Errorf("symbol is required for action %s", action)
		}
		return []string{action, "-p", p.RepoDir, "-j", "-l", strconv.Itoa(codeGraphLimit(args, 20)), symbol}, nil
	case "impact":
		symbol := strings.TrimSpace(getCodeGraphString(args, "symbol"))
		if symbol == "" {
			return nil, errors.New("symbol is required for action impact")
		}
		return []string{"impact", "-p", p.RepoDir, "-j", "-d", strconv.Itoa(codeGraphDepth(args, 2)), symbol}, nil
	case "files":
		cmdArgs := []string{"files", "-p", p.RepoDir, "-j"}
		if filter := strings.TrimSpace(getCodeGraphString(args, "filter")); filter != "" {
			cmdArgs = append(cmdArgs, "--filter", filter)
		}
		if pattern := strings.TrimSpace(getCodeGraphString(args, "pattern")); pattern != "" {
			cmdArgs = append(cmdArgs, "--pattern", pattern)
		}
		format := strings.TrimSpace(getCodeGraphString(args, "format"))
		if format == "" {
			format = "tree"
		}
		switch format {
		case "tree", "flat", "grouped":
			cmdArgs = append(cmdArgs, "--format", format)
		default:
			return nil, errors.New("format must be one of tree, flat, grouped")
		}
		if maxDepth, ok := getCodeGraphInt(args, "max_depth"); ok && maxDepth > 0 {
			cmdArgs = append(cmdArgs, "--max-depth", strconv.Itoa(maxDepth))
		}
		if includeMetadata, ok := args["include_metadata"].(bool); ok && !includeMetadata {
			cmdArgs = append(cmdArgs, "--no-metadata")
		}
		return cmdArgs, nil
	case "affected":
		cmdArgs := []string{"affected", "-p", p.RepoDir, "-j", "-d", strconv.Itoa(codeGraphDepth(args, 5))}
		if filter := strings.TrimSpace(getCodeGraphString(args, "filter")); filter != "" {
			cmdArgs = append(cmdArgs, "--filter", filter)
		}
		cmdArgs = append(cmdArgs, getCodeGraphStringSlice(args, "files")...)
		return cmdArgs, nil
	default:
		return nil, errors.New("action must be one of query, callers, callees, impact, files, affected")
	}
}

// CheckCodeGraphAvailable verifies that CodeGraph can safely describe the same checkout.
func CheckCodeGraphAvailable(ctx context.Context, fr *FileReader, opts CodeGraphOptions) CodeGraphAvailability {
	if fr == nil || strings.TrimSpace(fr.RepoDir) == "" {
		return CodeGraphAvailability{Reason: "repository directory is empty"}
	}

	opts = normalizeCodeGraphOptions(opts)
	dbPath := filepath.Join(fr.RepoDir, ".codegraph", "codegraph.db")
	if st, err := os.Stat(dbPath); err != nil || st.IsDir() {
		return CodeGraphAvailability{Reason: ".codegraph/codegraph.db is missing"}
	}

	runCtx, cancel := context.WithTimeout(ctx, codeGraphTimeout)
	defer cancel()

	versionOut, versionErr, err := opts.Runner.Run(runCtx, fr.RepoDir, opts.Binary, "--version")
	if err != nil {
		msg := strings.TrimSpace(versionErr)
		if msg == "" {
			msg = err.Error()
		}
		return CodeGraphAvailability{Reason: "codegraph executable is unavailable: " + msg}
	}
	version := strings.TrimSpace(versionOut)
	if compareCodeGraphVersions(version, opts.MinVersion) < 0 {
		return CodeGraphAvailability{Reason: fmt.Sprintf("codegraph version %s is older than required %s", version, opts.MinVersion)}
	}

	if fr.Ref != "" {
		if ok, reason := codeGraphRefMatchesHead(runCtx, fr, opts.Runner); !ok {
			return CodeGraphAvailability{Reason: reason}
		}
	}

	statusOut, statusErr, err := opts.Runner.Run(runCtx, fr.RepoDir, opts.Binary, "status", "-j", fr.RepoDir)
	if err != nil {
		msg := strings.TrimSpace(statusErr)
		if msg == "" {
			msg = err.Error()
		}
		return CodeGraphAvailability{Reason: "codegraph status failed: " + msg}
	}
	var status codeGraphStatus
	if err := json.Unmarshal([]byte(statusOut), &status); err != nil {
		return CodeGraphAvailability{Reason: "codegraph status returned invalid JSON"}
	}
	if !status.Initialized {
		return CodeGraphAvailability{Reason: "codegraph index is not initialized"}
	}
	if status.PendingChanges.Added != 0 || status.PendingChanges.Modified != 0 || status.PendingChanges.Removed != 0 {
		return CodeGraphAvailability{Reason: "codegraph index has pending changes"}
	}
	if len(status.WorktreeMismatch) > 0 && string(status.WorktreeMismatch) != "null" {
		return CodeGraphAvailability{Reason: "codegraph index does not match the current worktree"}
	}

	return CodeGraphAvailability{Available: true}
}

type codeGraphStatus struct {
	Initialized      bool             `json:"initialized"`
	PendingChanges   codeGraphPending `json:"pendingChanges"`
	WorktreeMismatch json.RawMessage  `json:"worktreeMismatch"`
}

type codeGraphPending struct {
	Added    int `json:"added"`
	Modified int `json:"modified"`
	Removed  int `json:"removed"`
}

func normalizeCodeGraphOptions(opts CodeGraphOptions) CodeGraphOptions {
	if opts.Binary == "" {
		opts.Binary = defaultCodeGraphBinary
	}
	if opts.MinVersion == "" {
		opts.MinVersion = minCodeGraphVersion
	}
	if opts.Runner == nil {
		opts.Runner = execCodeGraphRunner{}
	}
	return opts
}

func codeGraphRefMatchesHead(ctx context.Context, fr *FileReader, runner CodeGraphCommandRunner) (bool, string) {
	head, stderr, err := runner.Run(ctx, fr.RepoDir, "git", "rev-parse", "--verify", "--end-of-options", "HEAD^{commit}")
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = err.Error()
		}
		return false, "unable to resolve HEAD: " + msg
	}

	target, stderr, err := runner.Run(ctx, fr.RepoDir, "git", "rev-parse", "--verify", "--end-of-options", fr.Ref+"^{commit}")
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = err.Error()
		}
		return false, "unable to resolve review ref: " + msg
	}

	if strings.TrimSpace(head) != strings.TrimSpace(target) {
		return false, "codegraph index is for the current checkout, which differs from the review ref"
	}
	return true, ""
}

func getCodeGraphString(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func getCodeGraphStringSlice(args map[string]any, key string) []string {
	items, _ := args[key].([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func getCodeGraphInt(args map[string]any, key string) (int, bool) {
	switch v := args[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}

func codeGraphLimit(args map[string]any, defaultValue int) int {
	n, ok := getCodeGraphInt(args, "limit")
	if !ok || n <= 0 {
		return defaultValue
	}
	if n > codeGraphMaxLimit {
		return codeGraphMaxLimit
	}
	return n
}

func codeGraphDepth(args map[string]any, defaultValue int) int {
	n, ok := getCodeGraphInt(args, "depth")
	if !ok || n <= 0 {
		return defaultValue
	}
	if n > codeGraphMaxDepth {
		return codeGraphMaxDepth
	}
	return n
}

func compareCodeGraphVersions(version, minimum string) int {
	a := parseCodeGraphVersion(version)
	b := parseCodeGraphVersion(minimum)
	for i := 0; i < len(a) || i < len(b); i++ {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	return 0
}

func parseCodeGraphVersion(version string) []int {
	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	fields := strings.FieldsFunc(version, func(r rune) bool {
		return r < '0' || r > '9'
	})
	parts := make([]int, 0, len(fields))
	for _, field := range fields {
		if field == "" {
			continue
		}
		n, err := strconv.Atoi(field)
		if err != nil {
			break
		}
		parts = append(parts, n)
	}
	return parts
}
