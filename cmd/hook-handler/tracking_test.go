package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eklemin/wf-agents/internal/config"
)

// ---------------------------------------------------------------------------
// resolveAgentName
// ---------------------------------------------------------------------------

func TestResolveAgentName(t *testing.T) {
	cases := []struct {
		name         string
		teammateName string
		agentType    string
		want         string
	}{
		{"prefers TeammateName", "dev-1", "dev-1", "dev-1"},
		{"fallback to AgentType", "", "developer-1", "developer-1"},
		{"both empty", "", "", ""},
		{"TeammateName only", "reviewer-1", "", "reviewer-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := claudeHookInput{
				TeammateName: tc.teammateName,
				AgentType:    tc.agentType,
			}
			got := resolveAgentName(input)
			if got != tc.want {
				t.Errorf("resolveAgentName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// matchesAnyPattern
// ---------------------------------------------------------------------------

func TestMatchesAnyPattern(t *testing.T) {
	cases := []struct {
		name     string
		cmd      string
		patterns []string
		want     bool
	}{
		{"exact match", "make lint", []string{"make lint"}, true},
		{"prefix with word boundary (2>&1)", "make lint 2>&1", []string{"make lint"}, true},
		{"no word boundary (lintfix)", "make lintfix", []string{"make lint"}, false},
		{"go test with args", "go test ./...", []string{"go test"}, true},
		{"go testing is not go test", "go testing", []string{"go test"}, false},
		{"no match in list", "npm test", []string{"make test", "go test"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchesAnyPattern(tc.cmd, tc.patterns)
			if got != tc.want {
				t.Errorf("matchesAnyPattern(%q, %v) = %v, want %v", tc.cmd, tc.patterns, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// matchesBashPatternPrefix
// ---------------------------------------------------------------------------

func TestMatchesBashPatternPrefix(t *testing.T) {
	cases := []struct {
		name    string
		cmd     string
		pattern string
		want    bool
	}{
		{"exact match", "make lint", "make lint", true},
		{"prefix with space", "make lint -v", "make lint", true},
		{"prefix with pipe", "make lint|tee", "make lint", true},
		{"not a prefix (lintcheck)", "make lintcheck", "make lint", false},
		{"prefix with semicolon", "make lint;echo done", "make lint", true},
		{"prefix with tab", "make lint\t-v", "make lint", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchesBashPatternPrefix(tc.cmd, tc.pattern)
			if got != tc.want {
				t.Errorf("matchesBashPatternPrefix(%q, %q) = %v, want %v", tc.cmd, tc.pattern, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// matchesAnyPattern with real default config patterns
// ---------------------------------------------------------------------------

// TestMatchesAnyPattern_DefaultConfigPatterns verifies that the default tracking
// patterns from config.DefaultConfig match as expected.
func TestMatchesAnyPattern_DefaultConfigPatterns(t *testing.T) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("config.DefaultConfig: %v", err)
	}

	lintPatterns := cfg.Tracking["lint"].Patterns
	testPatterns := cfg.Tracking["test"].Patterns

	cases := []struct {
		name     string
		cmd      string
		patterns []string
		want     bool
	}{
		{"go vet matches lint", "go vet ./...", lintPatterns, true},
		{"golangci-lint matches lint", "golangci-lint run", lintPatterns, true},
		{"go test matches test", "go test ./...", testPatterns, true},
		{"npm test matches test", "npm test", testPatterns, true},
		{"echo does not match lint", "echo hello", lintPatterns, false},
		{"echo does not match test", "echo hello", testPatterns, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchesAnyPattern(tc.cmd, tc.patterns)
			if got != tc.want {
				t.Errorf("matchesAnyPattern(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// evalTeammateIdleConfig
// ---------------------------------------------------------------------------

// writeIdleConfig writes a .wf-agents/workflow.yaml with custom teammate_idle rules to dir.
func writeIdleConfig(t *testing.T, dir, yaml string) {
	t.Helper()
	wfAgentsDir := filepath.Join(dir, ".wf-agents")
	if err := os.MkdirAll(wfAgentsDir, 0o755); err != nil {
		t.Fatalf("writeIdleConfig: mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wfAgentsDir, "workflow.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("writeIdleConfig: %v", err)
	}
}

// TestEvalTeammateIdleConfig_WithCommands verifies the full eval path using a
// temp config with command_ran idle rules.
func TestEvalTeammateIdleConfig_WithCommands(t *testing.T) {
	dir := t.TempDir()
	writeIdleConfig(t, dir, `
teammate_idle:
  - phase: DEVELOPING
    checks:
      - type: command_ran
        category: lint
        message: "Must run lint before going idle"
      - type: command_ran
        category: test
        message: "Must run tests before going idle"
`)

	t.Run("both commands ran — allowed", func(t *testing.T) {
		reason := evalTeammateIdleConfig(dir, "DEVELOPING", "developer-1", map[string]bool{
			"lint": true,
			"test": true,
		})
		if reason != "" {
			t.Errorf("expected no denial, got: %q", reason)
		}
	})

	t.Run("no commands ran — denied", func(t *testing.T) {
		reason := evalTeammateIdleConfig(dir, "DEVELOPING", "developer-1", map[string]bool{})
		if reason == "" {
			t.Error("expected denial when no commands ran, got empty string")
		}
	})

	t.Run("only lint ran — denied for missing test", func(t *testing.T) {
		reason := evalTeammateIdleConfig(dir, "DEVELOPING", "developer-1", map[string]bool{
			"lint": true,
		})
		if reason == "" {
			t.Error("expected denial when test has not run, got empty string")
		}
	})
}

// TestEvalTeammateIdleConfig_ReviewerFree verifies that a reviewer going idle in
// REVIEWING is denied until they send a completion summary via SendMessage.
func TestEvalTeammateIdleConfig_ReviewerFree(t *testing.T) {
	dir := t.TempDir()
	reason := evalTeammateIdleConfig(dir, "REVIEWING", "reviewer-1", nil)
	if reason == "" {
		t.Error("reviewer should be denied idle in REVIEWING until send_message check passes")
	}
}
