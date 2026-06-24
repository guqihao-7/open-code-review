package main

import (
	"testing"

	"github.com/open-code-review/open-code-review/internal/config/toolsconfig"
	"github.com/open-code-review/open-code-review/internal/tool"
)

func TestFilterCodeGraphToolEntriesUnavailable(t *testing.T) {
	entries := []toolsconfig.ToolConfigEntry{
		{Name: tool.CodeSearch.Name()},
		{Name: tool.CodeGraph.Name()},
		{Name: tool.FileRead.Name()},
	}

	filtered := filterCodeGraphToolEntries(entries, false)

	if len(filtered) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(filtered))
	}
	for _, entry := range filtered {
		if entry.Name == tool.CodeGraph.Name() {
			t.Fatal("codegraph tool entry should be hidden when unavailable")
		}
	}
}

func TestFilterCodeGraphToolEntriesAvailable(t *testing.T) {
	entries := []toolsconfig.ToolConfigEntry{
		{Name: tool.CodeSearch.Name()},
		{Name: tool.CodeGraph.Name()},
	}

	filtered := filterCodeGraphToolEntries(entries, true)

	if len(filtered) != len(entries) {
		t.Fatalf("expected all entries to remain, got %d", len(filtered))
	}
}

func TestBuildToolRegistryRegistersCodeGraphWhenAvailable(t *testing.T) {
	reg := buildToolRegistry(tool.NewCommentCollector(), &tool.FileReader{RepoDir: "/repo"}, tool.CodeGraphAvailability{Available: true})

	if _, ok := reg.Get(tool.CodeGraph.Name()); !ok {
		t.Fatal("expected codegraph provider to be registered")
	}
}

func TestBuildToolRegistrySkipsCodeGraphWhenUnavailable(t *testing.T) {
	reg := buildToolRegistry(tool.NewCommentCollector(), &tool.FileReader{RepoDir: "/repo"}, tool.CodeGraphAvailability{})

	if _, ok := reg.Get(tool.CodeGraph.Name()); ok {
		t.Fatal("did not expect codegraph provider to be registered")
	}
}
