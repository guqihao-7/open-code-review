package tool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCodeGraphBuildArgs_QueryWithKindAndLimit(t *testing.T) {
	p := NewCodeGraph("/repo")
	args, err := p.buildArgs(map[string]any{
		"action": "query",
		"search": "BuildToolDefs",
		"kind":   "function",
		"limit":  float64(100),
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := []string{"query", "-p", "/repo", "-j", "-l", "50", "-k", "function", "BuildToolDefs"}
	if !reflect.DeepEqual(args, expected) {
		t.Fatalf("expected %v, got %v", expected, args)
	}
}

func TestCodeGraphBuildArgs_Impact(t *testing.T) {
	p := NewCodeGraph("/repo")
	args, err := p.buildArgs(map[string]any{
		"action": "impact",
		"symbol": "Parse",
		"depth":  float64(3),
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := []string{"impact", "-p", "/repo", "-j", "-d", "3", "Parse"}
	if !reflect.DeepEqual(args, expected) {
		t.Fatalf("expected %v, got %v", expected, args)
	}
}

func TestCodeGraphExecuteUsesRunner(t *testing.T) {
	runner := &fakeCodeGraphRunner{
		responses: map[string]fakeCodeGraphResponse{
			"codegraph query -p /repo -j -l 10 BuildToolDefs": {stdout: `[{"name":"BuildToolDefs"}]`},
		},
	}
	p := &CodeGraphProvider{RepoDir: "/repo", Binary: "codegraph", Runner: runner}

	result, err := p.Execute(context.Background(), map[string]any{
		"action": "query",
		"search": "BuildToolDefs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != `[{"name":"BuildToolDefs"}]` {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestCheckCodeGraphAvailable_MissingDatabase(t *testing.T) {
	fr := &FileReader{RepoDir: t.TempDir()}

	availability := CheckCodeGraphAvailable(context.Background(), fr, CodeGraphOptions{
		Runner: &fakeCodeGraphRunner{},
	})

	if availability.Available {
		t.Fatal("expected CodeGraph to be unavailable without database")
	}
	if !strings.Contains(availability.Reason, "codegraph.db") {
		t.Fatalf("unexpected reason: %s", availability.Reason)
	}
}

func TestCheckCodeGraphAvailable_Success(t *testing.T) {
	dir := codeGraphRepoWithDB(t)
	runner := &fakeCodeGraphRunner{
		responses: map[string]fakeCodeGraphResponse{
			"codegraph --version":                                   {stdout: "0.9.9\n"},
			"codegraph status -j " + dir:                            {stdout: `{"initialized":true,"pendingChanges":{"added":0,"modified":0,"removed":0},"worktreeMismatch":null}`},
			"git rev-parse --verify --end-of-options HEAD^{commit}": {stdout: "abc\n"},
			"git rev-parse --verify --end-of-options abc^{commit}":  {stdout: "abc\n"},
		},
	}
	fr := &FileReader{RepoDir: dir, Ref: "abc", Mode: ModeCommit}

	availability := CheckCodeGraphAvailable(context.Background(), fr, CodeGraphOptions{
		Runner: runner,
	})

	if !availability.Available {
		t.Fatalf("expected available, got reason: %s", availability.Reason)
	}
}

func TestCheckCodeGraphAvailable_HidesWhenRefDiffersFromHead(t *testing.T) {
	dir := codeGraphRepoWithDB(t)
	runner := &fakeCodeGraphRunner{
		responses: map[string]fakeCodeGraphResponse{
			"codegraph --version": {stdout: "0.9.9\n"},
			"git rev-parse --verify --end-of-options HEAD^{commit}":   {stdout: "head\n"},
			"git rev-parse --verify --end-of-options target^{commit}": {stdout: "target\n"},
		},
	}
	fr := &FileReader{RepoDir: dir, Ref: "target", Mode: ModeRange}

	availability := CheckCodeGraphAvailable(context.Background(), fr, CodeGraphOptions{
		Runner: runner,
	})

	if availability.Available {
		t.Fatal("expected unavailable when review ref differs from HEAD")
	}
	if !strings.Contains(availability.Reason, "differs from the review ref") {
		t.Fatalf("unexpected reason: %s", availability.Reason)
	}
}

func TestCheckCodeGraphAvailable_HidesWhenVersionTooOld(t *testing.T) {
	dir := codeGraphRepoWithDB(t)
	runner := &fakeCodeGraphRunner{
		responses: map[string]fakeCodeGraphResponse{
			"codegraph --version": {stdout: "0.8.9\n"},
		},
	}

	availability := CheckCodeGraphAvailable(context.Background(), &FileReader{RepoDir: dir}, CodeGraphOptions{
		Runner: runner,
	})

	if availability.Available {
		t.Fatal("expected unavailable for old CodeGraph version")
	}
	if !strings.Contains(availability.Reason, "older than required") {
		t.Fatalf("unexpected reason: %s", availability.Reason)
	}
}

func codeGraphRepoWithDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	codeGraphDir := filepath.Join(dir, ".codegraph")
	if err := os.MkdirAll(codeGraphDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codeGraphDir, "codegraph.db"), []byte("db"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

type fakeCodeGraphRunner struct {
	responses map[string]fakeCodeGraphResponse
}

type fakeCodeGraphResponse struct {
	stdout string
	stderr string
	err    error
}

func (f *fakeCodeGraphRunner) Run(_ context.Context, _ string, name string, args ...string) (string, string, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	if resp, ok := f.responses[key]; ok {
		return resp.stdout, resp.stderr, resp.err
	}
	return "", "", errors.New("unexpected command: " + key)
}
