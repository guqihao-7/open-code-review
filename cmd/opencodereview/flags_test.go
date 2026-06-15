package main

import "testing"

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
