package toolsconfig

import "testing"

func TestLoadDefaultToolsIncludesCodeGraph(t *testing.T) {
	entries, err := Load("")
	if err != nil {
		t.Fatalf("Load default tools: %v", err)
	}

	for _, entry := range entries {
		if entry.Name == "codegraph" {
			if !entry.PlanTask || !entry.MainTask {
				t.Fatal("codegraph should be available in both plan and main task phases when exposed")
			}
			return
		}
	}
	t.Fatal("default tools should include codegraph")
}
