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
}

func TestParseReviewFlagsMergeSystemRuleDefaultFalse(t *testing.T) {
	opts, err := parseReviewFlags(nil)
	if err != nil {
		t.Fatalf("parseReviewFlags: %v", err)
	}
	if opts.mergeSystemRule {
		t.Fatal("expected mergeSystemRule to default to false")
	}
}

func TestParseReviewFlagsMergeSystemRule(t *testing.T) {
	opts, err := parseReviewFlags([]string{"--merge-sys-rule"})
	if err != nil {
		t.Fatalf("parseReviewFlags: %v", err)
	}
	if !opts.mergeSystemRule {
		t.Fatal("expected --merge-sys-rule to enable mergeSystemRule")
	}
}
