package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecClientMapsToolCalls(t *testing.T) {
	client := newExecHelperClient(t, "tool-call")
	resp, err := client.CompletionsWithCtx(context.Background(), ChatRequest{
		Model:    "cli-default",
		Messages: []Message{NewTextMessage("user", "review this diff")},
		Tools: []ToolDef{{
			Type: "function",
			Function: FunctionDef{
				Name:       "code_comment",
				Parameters: map[string]any{"type": "object"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("CompletionsWithCtx: %v", err)
	}
	if resp.Model != "cli-default" {
		t.Fatalf("Model = %q, want cli-default", resp.Model)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens == 0 {
		t.Fatal("expected estimated token usage")
	}
	calls := resp.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("ToolCalls = %d, want 1", len(calls))
	}
	if calls[0].ID == "" || !strings.HasPrefix(calls[0].ID, "ocr_exec_") {
		t.Errorf("generated tool call ID = %q", calls[0].ID)
	}
	if calls[0].Function.Name != "code_comment" {
		t.Errorf("tool name = %q, want code_comment", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"path":"main.go","line":7}` {
		t.Errorf("arguments = %s", calls[0].Function.Arguments)
	}
}

func TestExecClientExpandsPromptAndSchemaFiles(t *testing.T) {
	workingDir := t.TempDir()
	client := newExecHelperClient(t, "files", execPromptFileToken, execSchemaFileToken, execModelToken, execWorkingDirToken)
	client.cfg.WorkingDir = workingDir

	resp, err := client.CompletionsWithCtx(context.Background(), ChatRequest{
		Model:    "selected-model",
		Messages: []Message{NewTextMessage("user", "PROMPT_SENTINEL")},
	})
	if err != nil {
		t.Fatalf("CompletionsWithCtx: %v", err)
	}
	if got := resp.Content(); got != "files-ok" {
		t.Fatalf("Content = %q, want files-ok", got)
	}
}

func TestExecClientAcceptsJSONEncodedArguments(t *testing.T) {
	client := newExecHelperClient(t, "encoded-arguments")
	resp, err := client.CompletionsWithCtx(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("CompletionsWithCtx: %v", err)
	}
	if got := resp.ToolCalls()[0].Function.Arguments; got != `{"done":true}` {
		t.Fatalf("arguments = %s", got)
	}
}

func TestNormalizeExecArgumentsPreservesLargeIntegers(t *testing.T) {
	const input = `{"id":9007199254740993,"nested":{"value":9223372036854775807}}`
	got, err := normalizeExecArguments(json.RawMessage(input))
	if err != nil {
		t.Fatalf("normalizeExecArguments: %v", err)
	}
	if got != input {
		t.Fatalf("arguments = %s, want exact numeric values %s", got, input)
	}
}

func TestExecClientRejectsMalformedOutput(t *testing.T) {
	client := newExecHelperClient(t, "malformed")
	_, err := client.CompletionsWithCtx(context.Background(), ChatRequest{Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "invalid response") {
		t.Fatalf("error = %v, want invalid response", err)
	}
}

func TestExecClientRejectsMissingRequiredFields(t *testing.T) {
	client := newExecHelperClient(t, "missing-fields")
	_, err := client.CompletionsWithCtx(context.Background(), ChatRequest{Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "missing required field") {
		t.Fatalf("error = %v, want missing required field", err)
	}
}

func TestExecClientReportsCommandFailureWithoutMixingStderr(t *testing.T) {
	client := newExecHelperClient(t, "failure")
	_, err := client.CompletionsWithCtx(context.Background(), ChatRequest{Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "helper failure") {
		t.Fatalf("error = %v, want helper stderr", err)
	}
}

func TestExecClientHonorsContextCancellation(t *testing.T) {
	client := newExecHelperClient(t, "sleep")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := client.CompletionsWithCtx(ctx, ChatRequest{Model: "m"})
	if err != context.DeadlineExceeded {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
}

func TestMergeExecEnvOverridesExistingValue(t *testing.T) {
	got := mergeExecEnv([]string{"A=old", "B=keep"}, []string{"A=new", "C=added"})
	joined := strings.Join(got, "|")
	if joined != "A=new|B=keep|C=added" {
		t.Fatalf("mergeExecEnv = %q", joined)
	}
}

func newExecHelperClient(t *testing.T, mode string, extraArgs ...string) *ExecClient {
	t.Helper()
	args := []string{"-test.run=TestExecHelperProcess", "--", mode}
	args = append(args, extraArgs...)
	return NewExecClient(ExecClientConfig{
		Command:        os.Args[0],
		Args:           args,
		Env:            []string{"GO_WANT_OCR_EXEC_HELPER=1", "OCR_EXEC_ENV_SENTINEL=present"},
		Model:          "cli-default",
		Timeout:        2 * time.Second,
		MaxConcurrency: 1,
	})
}

func TestExecHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_OCR_EXEC_HELPER") != "1" {
		return
	}
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		fmt.Fprintln(os.Stderr, "missing helper mode")
		os.Exit(2)
	}
	mode := os.Args[separator+1]
	args := os.Args[separator+2:]

	switch mode {
	case "tool-call":
		input, _ := io.ReadAll(os.Stdin)
		if !strings.Contains(string(input), "review this diff") || os.Getenv("OCR_EXEC_ENV_SENTINEL") != "present" {
			fmt.Fprintln(os.Stderr, "missing prompt or env sentinel")
			os.Exit(3)
		}
		fmt.Print(`{"content":"","tool_calls":[{"id":"","name":"code_comment","arguments":{"path":"main.go","line":7}}]}`)
	case "files":
		if len(args) != 4 {
			fmt.Fprintf(os.Stderr, "files args = %v\n", args)
			os.Exit(4)
		}
		prompt, promptErr := os.ReadFile(args[0])
		schema, schemaErr := os.ReadFile(args[1])
		cwd, _ := os.Getwd()
		resolvedCwd, _ := filepath.EvalSymlinks(cwd)
		resolvedArgCwd, _ := filepath.EvalSymlinks(args[3])
		if promptErr != nil || schemaErr != nil || !strings.Contains(string(prompt), "PROMPT_SENTINEL") ||
			!strings.Contains(string(schema), `"tool_calls"`) || args[2] != "selected-model" ||
			filepath.Clean(resolvedArgCwd) != filepath.Clean(resolvedCwd) {
			fmt.Fprintf(os.Stderr, "invalid files invocation: promptErr=%v schemaErr=%v model=%q cwd=%q argCwd=%q\n", promptErr, schemaErr, args[2], cwd, args[3])
			os.Exit(5)
		}
		fmt.Println("agent startup log")
		fmt.Print(`{"content":"files-ok","tool_calls":[]}`)
	case "encoded-arguments":
		fmt.Print(`{"content":"","tool_calls":[{"id":"call-1","name":"task_done","arguments":"{\"done\":true}"}]}`)
	case "malformed":
		fmt.Print("not-json")
	case "missing-fields":
		fmt.Print(`{"content":"no tool_calls"}`)
	case "failure":
		fmt.Fprint(os.Stderr, "helper failure")
		os.Exit(7)
	case "sleep":
		time.Sleep(5 * time.Second)
		fmt.Print(`{"content":"late","tool_calls":[]}`)
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode %q\n", mode)
		os.Exit(8)
	}
	os.Exit(0)
}
