package main

import "testing"

func TestParseReviewFlagsModelOverride(t *testing.T) {
	opts, err := parseReviewFlags([]string{"--model", "claude-opus-4-6"})
	if err != nil {
		t.Fatalf("parseReviewFlags: %v", err)
	}

	if opts.model != "claude-opus-4-6" {
		t.Errorf("model = %q, want %q", opts.model, "claude-opus-4-6")
	}
	if opts.outputFormat != "text" {
		t.Errorf("outputFormat = %q, want %q", opts.outputFormat, "text")
	}
	if opts.audience != "human" {
		t.Errorf("audience = %q, want %q", opts.audience, "human")
	}
	if opts.llmTimeout != 0 {
		t.Errorf("llmTimeout = %d, want 0", opts.llmTimeout)
	}
}

func TestParseReviewFlagsLLMTimeout(t *testing.T) {
	opts, err := parseReviewFlags([]string{"--llm-timeout", "300"})
	if err != nil {
		t.Fatalf("parseReviewFlags: %v", err)
	}

	if opts.llmTimeout != 300 {
		t.Errorf("llmTimeout = %d, want 300", opts.llmTimeout)
	}
}

func TestParseReviewFlagsRejectsNegativeLLMTimeout(t *testing.T) {
	_, err := parseReviewFlags([]string{"--llm-timeout", "-1"})
	if err == nil {
		t.Fatal("expected negative --llm-timeout to fail")
	}
}
