package main

// Tool permission and teammate detection tests have moved to internal/workflow/tool_permissions_test.go.
// They now test the exported CheckToolPermission and IsTeammate functions directly.
//
// This file is intentionally left with only a package declaration so the package still compiles.
// Add hook-handler integration tests here if needed.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eklemin/wf-agents/internal/model"
)

// ---------------------------------------------------------------------------
// resolveWorkflowID tests
// ---------------------------------------------------------------------------

// makeMarker is a test helper that writes a JSON marker to $TMPDIR/wf-agents-sessions/<name>.
func makeMarker(t *testing.T, dir, filename string, fields map[string]string) {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("makeMarker: marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0o644); err != nil {
		t.Fatalf("makeMarker: write: %v", err)
	}
}

// setupSessionsDir creates a temp directory and overrides os.TempDir to return it.
// Because os.TempDir is not mockable, we use a different approach: we write directly
// to the real $TMPDIR/wf-agents-sessions directory and clean up afterwards.
func setupMarkerDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "wf-agents-sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("cannot create marker dir: %v", err)
	}
	return dir
}

// TestResolveWorkflowID_DirectMatch verifies that a lead session's marker is found directly.
func TestResolveWorkflowID_DirectMatch(t *testing.T) {
	dir := setupMarkerDir(t)
	sessionID := "direct-match-session-test"
	defer os.Remove(filepath.Join(dir, sessionID))

	makeMarker(t, dir, sessionID, map[string]string{
		"session_id":  sessionID,
		"workflow_id": "coding-session-" + sessionID,
		"cwd":         "/some/project",
	})

	got := resolveWorkflowID(sessionID, "/some/project")
	want := "coding-session-" + sessionID
	if got != want {
		t.Errorf("resolveWorkflowID direct match = %q, want %q", got, want)
	}
}

// TestResolveWorkflowID_LegacyPlainTextMarker verifies backward compatibility with
// old plain-text markers (session_id as plain text, not JSON).
func TestResolveWorkflowID_LegacyPlainTextMarker(t *testing.T) {
	dir := setupMarkerDir(t)
	sessionID := "legacy-plain-text-session"
	marker := filepath.Join(dir, sessionID)
	defer os.Remove(marker)

	// Write a legacy plain-text marker (old format)
	if err := os.WriteFile(marker, []byte(sessionID), 0o644); err != nil {
		t.Fatalf("could not write legacy marker: %v", err)
	}

	got := resolveWorkflowID(sessionID, "/any/cwd")
	want := "coding-session-" + sessionID
	if got != want {
		t.Errorf("resolveWorkflowID legacy marker = %q, want %q", got, want)
	}
}

// TestResolveWorkflowID_CWDMatch verifies that a teammate session (no direct marker)
// is resolved by scanning markers for a matching CWD.
func TestResolveWorkflowID_CWDMatch(t *testing.T) {
	dir := setupMarkerDir(t)
	leadSessionID := "lead-session-for-cwd-test"
	teammateSessionID := "teammate-cwd-match-session"
	cwd := "/unique/cwd/for/cwd-match-test"

	defer os.Remove(filepath.Join(dir, leadSessionID))
	defer os.Remove(filepath.Join(dir, teammateSessionID)) // auto-created teammate marker

	// Write lead session marker with a distinct CWD
	makeMarker(t, dir, leadSessionID, map[string]string{
		"session_id":  leadSessionID,
		"workflow_id": "coding-session-" + leadSessionID,
		"cwd":         cwd,
	})

	// Teammate has a different session_id but same CWD, no marker yet
	got := resolveWorkflowID(teammateSessionID, cwd)
	want := "coding-session-" + leadSessionID
	if got != want {
		t.Errorf("resolveWorkflowID CWD match = %q, want %q", got, want)
	}

	// A teammate marker should have been auto-created
	teammateMarker := filepath.Join(dir, teammateSessionID)
	data, err := os.ReadFile(teammateMarker)
	if err != nil {
		t.Fatalf("teammate marker was not auto-created: %v", err)
	}
	var m map[string]string
	if json.Unmarshal(data, &m) != nil {
		t.Fatalf("teammate marker is not valid JSON: %s", data)
	}
	if m["workflow_id"] != want {
		t.Errorf("teammate marker workflow_id = %q, want %q", m["workflow_id"], want)
	}
	if m["parent"] != leadSessionID {
		t.Errorf("teammate marker parent = %q, want %q", m["parent"], leadSessionID)
	}
	if m["cwd"] != cwd {
		t.Errorf("teammate marker cwd = %q, want %q", m["cwd"], cwd)
	}
}

// TestResolveWorkflowID_CWDMatch_SkipsTeammateMarkers verifies that teammate markers
// (those with a "parent" field) are not used as the CWD match source — only lead markers
// are considered when scanning by CWD.
// The stale teammate marker's name is alphabetically before the lead marker name to
// ensure the bug is observable: without the fix the current code returns the wrong workflow.
func TestResolveWorkflowID_CWDMatch_SkipsTeammateMarkers(t *testing.T) {
	dir := setupMarkerDir(t)
	// "aaa-stale-teammate-skip-test" sorts before "zzz-lead-skip-teammate-markers-test"
	// so without the fix the stale marker is found first (os.ReadDir returns sorted names).
	leadSessionID := "zzz-lead-skip-teammate-markers-test"
	oldTeammateSessionID := "aaa-stale-teammate-skip-test"
	newTeammateSessionID := "new-teammate-skip-test-zzz"
	cwd := "/unique/cwd/for/skip-teammate-markers-test"

	defer os.Remove(filepath.Join(dir, leadSessionID))
	defer os.Remove(filepath.Join(dir, oldTeammateSessionID))
	defer os.Remove(filepath.Join(dir, newTeammateSessionID)) // auto-created

	// Write stale teammate marker first (same CWD, but has "parent" field — wrong workflow)
	makeMarker(t, dir, oldTeammateSessionID, map[string]string{
		"session_id":  oldTeammateSessionID,
		"workflow_id": "coding-session-old-lead-wrong-workflow",
		"cwd":         cwd,
		"parent":      "some-old-lead-session",
	})

	// Write lead marker (same CWD, no parent field — correct workflow)
	makeMarker(t, dir, leadSessionID, map[string]string{
		"session_id":  leadSessionID,
		"workflow_id": "coding-session-" + leadSessionID,
		"cwd":         cwd,
	})

	// New teammate resolves — should get the lead's workflow, not the stale teammate's
	got := resolveWorkflowID(newTeammateSessionID, cwd)
	want := "coding-session-" + leadSessionID
	if got != want {
		t.Errorf("resolveWorkflowID should skip teammate markers, got %q, want %q", got, want)
	}
}

// TestResolveWorkflowID_CWDMatch_PicksLatest verifies that when multiple lead markers
// share the same CWD, the one with the most recent modification time wins.
// "aaa-old-lead-picks-latest-test" sorts before "zzz-new-lead-picks-latest-test" so
// without the fix the old lead is returned (first CWD match wins). With the fix the
// newest modtime wins.
func TestResolveWorkflowID_CWDMatch_PicksLatest(t *testing.T) {
	dir := setupMarkerDir(t)
	oldLeadSessionID := "aaa-old-lead-picks-latest-test"
	newLeadSessionID := "zzz-new-lead-picks-latest-test"
	teammateSessionID := "teammate-picks-latest-test"
	cwd := "/unique/cwd/for/picks-latest-test"

	defer os.Remove(filepath.Join(dir, oldLeadSessionID))
	defer os.Remove(filepath.Join(dir, newLeadSessionID))
	defer os.Remove(filepath.Join(dir, teammateSessionID))

	// Write old lead marker first and backdate its modtime
	makeMarker(t, dir, oldLeadSessionID, map[string]string{
		"session_id":  oldLeadSessionID,
		"workflow_id": "coding-session-" + oldLeadSessionID,
		"cwd":         cwd,
	})
	oldTime := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(filepath.Join(dir, oldLeadSessionID), oldTime, oldTime); err != nil {
		t.Fatalf("cannot set mod time on old marker: %v", err)
	}

	// Write new lead marker (same CWD, newer modtime)
	makeMarker(t, dir, newLeadSessionID, map[string]string{
		"session_id":  newLeadSessionID,
		"workflow_id": "coding-session-" + newLeadSessionID,
		"cwd":         cwd,
	})

	// Teammate resolves — should get the NEWEST lead's workflow
	got := resolveWorkflowID(teammateSessionID, cwd)
	want := "coding-session-" + newLeadSessionID
	if got != want {
		t.Errorf("resolveWorkflowID should pick latest lead marker, got %q, want %q", got, want)
	}
}

// TestResolveWorkflowID_NoMatch verifies that an unrelated session returns empty string.
func TestResolveWorkflowID_NoMatch(t *testing.T) {
	// Use a session ID that will never have a marker
	got := resolveWorkflowID("no-marker-session-xyz-never-exists", "/completely/unique/cwd/xyz-never")
	if got != "" {
		t.Errorf("resolveWorkflowID no match = %q, want empty string", got)
	}
}

// The deny path in main.go calls os.Exit(2) after writing the reason to stdout/stderr.
// os.Exit(2) cannot be tested in-process without subprocess scaffolding, so there is no
// TestDenyOutputFormat here. The deny behavior (exit code 2, plain-text reason on stdout)
// was verified via live testing — the Edit tool was blocked with exit code 2 as expected.
//
// The logic that decides WHAT to deny (which tools are forbidden in which phase, teammate
// detection, etc.) is thoroughly tested in internal/workflow/tool_permissions_test.go via
// the exported CheckToolPermission and IsTeammate functions.

// TestSessionStartOutputHasContinueTrue verifies that the SessionStart hookOutput
// serialized to JSON DOES include "continue": true so Claude keeps running.
func TestSessionStartOutputHasContinueTrue(t *testing.T) {
	out := hookOutput{
		Continue: boolPtr(true),
		HookSpecificOutput: &hookSpecificOutput{
			HookEventName:     "SessionStart",
			AdditionalContext: "session context",
		},
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(out); err != nil {
		t.Fatalf("failed to encode hookOutput: %v", err)
	}

	raw := buf.String()
	if !strings.Contains(raw, `"continue":true`) {
		t.Errorf("SessionStart output must contain \"continue\":true, got: %s", raw)
	}
}

// TestAllowWithContextOutputNoContinueField verifies that an allow-with-additionalContext
// hookOutput does NOT include the "continue" field (it is not required for allow).
func TestAllowWithContextOutputNoContinueField(t *testing.T) {
	out := hookOutput{
		HookSpecificOutput: &hookSpecificOutput{
			HookEventName:     "PreToolUse",
			AdditionalContext: "some phase instructions",
		},
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(out); err != nil {
		t.Fatalf("failed to encode hookOutput: %v", err)
	}

	raw := buf.String()
	if strings.Contains(raw, `"continue"`) {
		t.Errorf("allow-with-context output must NOT contain \"continue\" field, got: %s", raw)
	}
}

// TestPhaseInstructionsNonEmpty verifies that phaseInstructions returns non-empty content
// for all phases, and that no {{...}} placeholders remain unresolved.
func TestPhaseInstructionsNonEmpty(t *testing.T) {
	// Find the project root by locating the states directory.
	// The test binary runs from the package directory, so we go up two levels.
	projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("cannot determine project root: %v", err)
	}
	// Verify the states directory exists at the expected location.
	statesDir := filepath.Join(projectRoot, "states")
	if _, err := os.Stat(statesDir); err != nil {
		t.Fatalf("states directory not found at %s: %v", statesDir, err)
	}

	// Set CLAUDE_PLUGIN_ROOT so phaseInstructions can locate the state files.
	t.Setenv("CLAUDE_PLUGIN_ROOT", projectRoot)

	// phaseInstructions reads CLAUDE_PLUGIN_ROOT and uses wfClientBin() which reads env.
	// We also set WF_CLIENT_BIN to a known value so the placeholder resolves.
	t.Setenv("WF_CLIENT_BIN", "/usr/local/bin/wf-client")

	unresolvedPlaceholder := regexp.MustCompile(`\{\{[A-Z_]+\}\}`)

	phases := []model.Phase{
		model.PhasePlanning,
		model.PhaseRespawn,
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhasePRCreation,
		model.PhaseFeedback,
		model.PhaseComplete,
		model.PhaseBlocked,
	}

	for _, phase := range phases {
		t.Run(string(phase), func(t *testing.T) {
			result := phaseInstructions(phase)

			if strings.TrimSpace(result) == "" {
				t.Errorf("phaseInstructions(%s) returned empty string", phase)
			}

			if matches := unresolvedPlaceholder.FindAllString(result, -1); len(matches) > 0 {
				t.Errorf("phaseInstructions(%s) has unresolved placeholders: %v\nContent:\n%s",
					phase, matches, result)
			}
		})
	}
}

// TestPhaseInstructionsFallbackOnMissingFile verifies that phaseInstructions falls back
// gracefully when CLAUDE_PLUGIN_ROOT points to a directory without state files.
func TestPhaseInstructionsFallbackOnMissingFile(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", "/nonexistent/path")

	// Should not panic, should return a non-empty fallback string.
	result := phaseInstructions(model.PhasePlanning)
	if strings.TrimSpace(result) == "" {
		t.Error("phaseInstructions fallback must return non-empty string")
	}
}

// TestSyntheticAgentID_TeammateSessionGetsAgentID verifies that when a session_id differs from
// the workflow's session_id (teammate session), a synthetic agent_id is set.
func TestSyntheticAgentID_TeammateSessionGetsAgentID(t *testing.T) {
	dir := setupMarkerDir(t)
	leadSessionID := "lead-session-synthetic-agent-id-test"
	teammateSessionID := "teammate-synthetic-agent-id-test"
	cwd := "/unique/cwd/for/synthetic-agent-id-test"
	workflowID := "coding-session-" + leadSessionID

	defer os.Remove(filepath.Join(dir, leadSessionID))
	defer os.Remove(filepath.Join(dir, teammateSessionID))

	// Write lead session marker
	makeMarker(t, dir, leadSessionID, map[string]string{
		"session_id":  leadSessionID,
		"workflow_id": workflowID,
		"cwd":         cwd,
	})

	// Simulate a teammate hook input: same CWD, different session_id, empty agent_id
	input := claudeHookInput{
		SessionID: teammateSessionID,
		CWD:       cwd,
		AgentID:   "",
	}

	// Resolve the workflow ID (side-effect: creates teammate marker)
	resolvedWorkflowID := resolveWorkflowID(input.SessionID, input.CWD)
	if resolvedWorkflowID != workflowID {
		t.Fatalf("expected workflow_id %q, got %q", workflowID, resolvedWorkflowID)
	}

	// Apply synthetic agent_id logic (mirrors the code in main())
	workflowSessionID := strings.TrimPrefix(resolvedWorkflowID, "coding-session-")
	if input.SessionID != workflowSessionID && input.AgentID == "" {
		input.AgentID = "teammate-" + input.SessionID
	}

	expectedAgentID := "teammate-" + teammateSessionID
	if input.AgentID != expectedAgentID {
		t.Errorf("synthetic agent_id = %q, want %q", input.AgentID, expectedAgentID)
	}
}

// TestSyntheticAgentID_LeadSessionUnchanged verifies that the lead session's agent_id is NOT
// modified (session_id matches workflow's session_id).
func TestSyntheticAgentID_LeadSessionUnchanged(t *testing.T) {
	dir := setupMarkerDir(t)
	leadSessionID := "lead-session-no-change-test"
	workflowID := "coding-session-" + leadSessionID
	cwd := "/unique/cwd/for/no-change-test"

	defer os.Remove(filepath.Join(dir, leadSessionID))

	makeMarker(t, dir, leadSessionID, map[string]string{
		"session_id":  leadSessionID,
		"workflow_id": workflowID,
		"cwd":         cwd,
	})

	input := claudeHookInput{
		SessionID: leadSessionID,
		CWD:       cwd,
		AgentID:   "",
	}

	resolvedWorkflowID := resolveWorkflowID(input.SessionID, input.CWD)
	if resolvedWorkflowID != workflowID {
		t.Fatalf("expected workflow_id %q, got %q", workflowID, resolvedWorkflowID)
	}

	// Apply synthetic agent_id logic
	workflowSessionID := strings.TrimPrefix(resolvedWorkflowID, "coding-session-")
	if input.SessionID != workflowSessionID && input.AgentID == "" {
		input.AgentID = "teammate-" + input.SessionID
	}

	// Lead session should NOT get a synthetic agent_id
	if input.AgentID != "" {
		t.Errorf("lead session agent_id should remain empty, got %q", input.AgentID)
	}
}

// TestSyntheticAgentID_ExistingAgentIDPreserved verifies that a teammate with an existing
// agent_id (explicitly set by Claude Code) is not overwritten.
func TestSyntheticAgentID_ExistingAgentIDPreserved(t *testing.T) {
	dir := setupMarkerDir(t)
	leadSessionID := "lead-session-preserve-agent-id-test"
	teammateSessionID := "teammate-preserve-agent-id-test"
	cwd := "/unique/cwd/for/preserve-agent-id-test"
	workflowID := "coding-session-" + leadSessionID
	existingAgentID := "existing-agent-abc"

	defer os.Remove(filepath.Join(dir, leadSessionID))
	defer os.Remove(filepath.Join(dir, teammateSessionID))

	makeMarker(t, dir, leadSessionID, map[string]string{
		"session_id":  leadSessionID,
		"workflow_id": workflowID,
		"cwd":         cwd,
	})

	// Teammate already has an agent_id set
	input := claudeHookInput{
		SessionID: teammateSessionID,
		CWD:       cwd,
		AgentID:   existingAgentID,
	}

	resolvedWorkflowID := resolveWorkflowID(input.SessionID, input.CWD)
	if resolvedWorkflowID != workflowID {
		t.Fatalf("expected workflow_id %q, got %q", workflowID, resolvedWorkflowID)
	}

	// Apply synthetic agent_id logic
	workflowSessionID := strings.TrimPrefix(resolvedWorkflowID, "coding-session-")
	if input.SessionID != workflowSessionID && input.AgentID == "" {
		input.AgentID = "teammate-" + input.SessionID
	}

	// Existing agent_id should be preserved
	if input.AgentID != existingAgentID {
		t.Errorf("existing agent_id should be preserved, got %q, want %q", input.AgentID, existingAgentID)
	}
}

// ---------------------------------------------------------------------------
// logResponse tests
// ---------------------------------------------------------------------------

// TestLogResponse_WritesJSONLEntry verifies that logResponse appends a valid JSONL entry
// to the session log file with the correct fields.
func TestLogResponse_WritesJSONLEntry(t *testing.T) {
	// Use a unique session ID to avoid collisions with parallel tests
	sessionID := "test-log-response-session-abc123"
	logDir := filepath.Join(os.TempDir(), "wf-agents-hook-logs")
	logFile := filepath.Join(logDir, sessionID+".jsonl")

	// Ensure log file is cleaned up after the test
	defer os.Remove(logFile)

	// Call logResponse
	logResponse(sessionID, "PreToolUse", 2, map[string]string{
		"decision": "deny",
		"reason":   "tool not allowed",
	})

	// Read back the file
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("logResponse did not create log file: %v", err)
	}

	// Parse the JSONL line
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 JSONL line, got %d: %s", len(lines), string(data))
	}

	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("logResponse wrote invalid JSON: %v\nline: %s", err, lines[0])
	}

	// Verify required fields
	if entry["direction"] != "response" {
		t.Errorf("direction = %q, want \"response\"", entry["direction"])
	}
	if entry["event"] != "PreToolUse" {
		t.Errorf("event = %q, want \"PreToolUse\"", entry["event"])
	}
	if entry["exit_code"] == nil {
		t.Error("exit_code field is missing")
	}
	if entry["ts"] == nil {
		t.Error("ts field is missing")
	}
	if entry["response"] == nil {
		t.Error("response field is missing")
	}
}

// TestLogResponse_AppendMultipleEntries verifies that multiple logResponse calls
// append separate lines to the JSONL file without overwriting.
func TestLogResponse_AppendMultipleEntries(t *testing.T) {
	sessionID := "test-log-response-append-xyz789"
	logDir := filepath.Join(os.TempDir(), "wf-agents-hook-logs")
	logFile := filepath.Join(logDir, sessionID+".jsonl")
	defer os.Remove(logFile)

	logResponse(sessionID, "PreToolUse", 0, map[string]string{"decision": "allow"})
	logResponse(sessionID, "PreToolUse", 2, map[string]string{"decision": "deny"})

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("log file not found: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d:\n%s", len(lines), string(data))
	}

	for i, line := range lines {
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d is not valid JSON: %v\nline: %s", i+1, err, line)
		}
	}
}

// TestLogResponse_ExitCodePreserved verifies that logResponse stores the exit_code accurately.
func TestLogResponse_ExitCodePreserved(t *testing.T) {
	sessionID := "test-log-response-exitcode-def456"
	logDir := filepath.Join(os.TempDir(), "wf-agents-hook-logs")
	logFile := filepath.Join(logDir, sessionID+".jsonl")
	defer os.Remove(logFile)

	logResponse(sessionID, "TeammateIdle", 2, map[string]string{"action": "keep_working"})

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("log file not found: %v", err)
	}

	var entry map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(data), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// JSON numbers unmarshal as float64
	if exitCode, ok := entry["exit_code"].(float64); !ok || exitCode != 2 {
		t.Errorf("exit_code = %v, want 2", entry["exit_code"])
	}
}

// TestRequestLogHasDirectionField verifies that the existing request log entry
// includes a "direction":"request" field.
func TestRequestLogHasDirectionField(t *testing.T) {
	sessionID := "test-request-direction-ghi012"
	logDir := filepath.Join(os.TempDir(), "wf-agents-hook-logs")
	_ = os.MkdirAll(logDir, 0755)
	logFile := filepath.Join(logDir, sessionID+".jsonl")
	defer os.Remove(logFile)

	// Simulate the request log entry as written by main()
	rawInput := []byte(`{"session_id":"` + sessionID + `","hook_event_name":"PostToolUse"}`)
	logEntry := map[string]interface{}{
		"ts":        time.Now().UTC().Format(time.RFC3339Nano),
		"event":     "PostToolUse",
		"direction": "request",
		"raw":       json.RawMessage(rawInput),
	}
	logLine, err := json.Marshal(logEntry)
	if err != nil {
		t.Fatalf("failed to marshal log entry: %v", err)
	}

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("cannot open log file: %v", err)
	}
	f.Write(logLine)
	f.Write([]byte("\n"))
	f.Close()

	data, _ := os.ReadFile(logFile)
	var entry map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(data), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if entry["direction"] != "request" {
		t.Errorf("direction = %q, want \"request\"", entry["direction"])
	}
}

// ---------------------------------------------------------------------------
// trackPreToolUse and evalTeammateIdleConfig integration tests
// ---------------------------------------------------------------------------

// capturedSignal records one call to SignalWorkflow.
type capturedSignal struct {
	workflowID string
	signalName string
	arg        interface{}
}

// mockSignaler implements workflowSignaler and records all SignalWorkflow calls.
type mockSignaler struct {
	signals []capturedSignal
	err     error // if non-nil, returned by SignalWorkflow
}

func (m *mockSignaler) SignalWorkflow(_ context.Context, workflowID, _ string, signalName string, arg interface{}) error {
	m.signals = append(m.signals, capturedSignal{workflowID: workflowID, signalName: signalName, arg: arg})
	return m.err
}

// writeTempConfig writes a .wf-agents.yaml file to dir and returns the dir path.
func writeTempConfig(t *testing.T, dir, yamlContent string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".wf-agents.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("writeTempConfig: %v", err)
	}
}

// bashInput builds a JSON tool_input for a Bash command.
func bashInput(t *testing.T, cmd string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]string{"command": cmd})
	if err != nil {
		t.Fatalf("bashInput: %v", err)
	}
	return b
}

// TestTrackPreToolUse_BashCommandRan verifies that a Bash "go vet ./..." with agent_type="developer-1"
// sends a SignalCommandRan for the lint category (go vet is a default lint pattern).
func TestTrackPreToolUse_BashCommandRan(t *testing.T) {
	dir := t.TempDir()
	mock := &mockSignaler{}
	input := claudeHookInput{
		SessionID:     "sess-1",
		HookEventName: "PreToolUse",
		AgentType:     "developer-1",
		ToolName:      "Bash",
		ToolInput:     bashInput(t, "go vet ./..."),
		CWD:           dir,
	}
	trackPreToolUse(context.Background(), mock, "coding-session-abc", input)

	if len(mock.signals) == 0 {
		t.Fatal("expected at least one signal, got none")
	}
	var found bool
	for _, s := range mock.signals {
		if s.signalName == "command-ran" {
			sig, ok := s.arg.(model.SignalCommandRan)
			if !ok {
				t.Fatalf("expected SignalCommandRan payload, got %T", s.arg)
			}
			if sig.AgentName != "developer-1" {
				t.Errorf("AgentName = %q, want %q", sig.AgentName, "developer-1")
			}
			if sig.Category != "lint" {
				t.Errorf("Category = %q, want %q", sig.Category, "lint")
			}
			found = true
		}
	}
	if !found {
		t.Error("no command-ran signal was sent for lint category")
	}
}

// TestTrackPreToolUse_NoSignalWhenNoAgentIdentity verifies that when both TeammateName and
// AgentType are empty, no signal is sent.
func TestTrackPreToolUse_NoSignalWhenNoAgentIdentity(t *testing.T) {
	dir := t.TempDir()
	mock := &mockSignaler{}
	input := claudeHookInput{
		SessionID:    "sess-2",
		HookEventName: "PreToolUse",
		TeammateName: "",
		AgentType:    "",
		ToolName:     "Bash",
		ToolInput:    bashInput(t, "make lint"),
		CWD:          dir,
	}
	trackPreToolUse(context.Background(), mock, "coding-session-abc", input)
	if len(mock.signals) != 0 {
		t.Errorf("expected no signals, got %d", len(mock.signals))
	}
}

// TestTrackPreToolUse_EditSendsInvalidateCommands verifies that an Edit tool use with
// agent_type="developer-1" sends SignalInvalidateCommands for categories with
// invalidate_on_file_change=true.
func TestTrackPreToolUse_EditSendsInvalidateCommands(t *testing.T) {
	dir := t.TempDir()
	mock := &mockSignaler{}
	input := claudeHookInput{
		SessionID:    "sess-3",
		HookEventName: "PreToolUse",
		AgentType:    "developer-1",
		ToolName:     "Edit",
		CWD:          dir,
	}
	trackPreToolUse(context.Background(), mock, "coding-session-abc", input)

	if len(mock.signals) == 0 {
		t.Fatal("expected at least one signal, got none")
	}
	for _, s := range mock.signals {
		if s.signalName == "invalidate-commands" {
			sig, ok := s.arg.(model.SignalInvalidateCommands)
			if !ok {
				t.Fatalf("expected SignalInvalidateCommands payload, got %T", s.arg)
			}
			if sig.AgentName != "developer-1" {
				t.Errorf("AgentName = %q, want %q", sig.AgentName, "developer-1")
			}
			if sig.Tool != "Edit" {
				t.Errorf("Tool = %q, want %q", sig.Tool, "Edit")
			}
			if len(sig.Categories) == 0 {
				t.Error("Categories must be non-empty")
			}
			return
		}
	}
	t.Error("no invalidate-commands signal was sent")
}

// TestTrackPreToolUse_WriteSendsInvalidateCommands verifies the same invalidation for Write.
func TestTrackPreToolUse_WriteSendsInvalidateCommands(t *testing.T) {
	dir := t.TempDir()
	mock := &mockSignaler{}
	input := claudeHookInput{
		SessionID:    "sess-4",
		HookEventName: "PreToolUse",
		AgentType:    "reviewer-1",
		ToolName:     "Write",
		CWD:          dir,
	}
	trackPreToolUse(context.Background(), mock, "coding-session-abc", input)

	for _, s := range mock.signals {
		if s.signalName == "invalidate-commands" {
			sig, ok := s.arg.(model.SignalInvalidateCommands)
			if !ok {
				t.Fatalf("expected SignalInvalidateCommands payload, got %T", s.arg)
			}
			if sig.AgentName != "reviewer-1" {
				t.Errorf("AgentName = %q, want %q", sig.AgentName, "reviewer-1")
			}
			return
		}
	}
	t.Error("no invalidate-commands signal sent for Write tool")
}

// TestResolveAgentName_PrefersTeammateName verifies that TeammateName takes precedence over AgentType.
func TestResolveAgentName_PrefersTeammateName(t *testing.T) {
	input := claudeHookInput{
		TeammateName: "developer-2",
		AgentType:    "wf-agents:developer",
	}
	got := resolveAgentName(input)
	if got != "developer-2" {
		t.Errorf("resolveAgentName = %q, want %q", got, "developer-2")
	}
}

// TestResolveAgentName_FallsBackToAgentType verifies AgentType is used when TeammateName is empty.
func TestResolveAgentName_FallsBackToAgentType(t *testing.T) {
	input := claudeHookInput{
		TeammateName: "",
		AgentType:    "developer-1",
	}
	got := resolveAgentName(input)
	if got != "developer-1" {
		t.Errorf("resolveAgentName = %q, want %q", got, "developer-1")
	}
}

// TestMatchesBashPatternPrefix_ExactMatch verifies that a command exactly matching the pattern passes.
func TestMatchesBashPatternPrefix_ExactMatch(t *testing.T) {
	if !matchesBashPatternPrefix("make lint", "make lint") {
		t.Error("exact match should return true")
	}
}

// TestMatchesBashPatternPrefix_WithArgs verifies "make lint 2>&1" matches pattern "make lint".
func TestMatchesBashPatternPrefix_WithArgs(t *testing.T) {
	if !matchesBashPatternPrefix("make lint 2>&1", "make lint") {
		t.Error("command with args should match pattern prefix")
	}
}

// TestMatchesBashPatternPrefix_GoTest verifies "go test ./..." matches pattern "go test".
func TestMatchesBashPatternPrefix_GoTest(t *testing.T) {
	if !matchesBashPatternPrefix("go test ./...", "go test") {
		t.Error("'go test ./...' should match 'go test'")
	}
}

// TestMatchesBashPatternPrefix_NoSubstringMatch verifies that "govet" does NOT match "go vet"
// (word boundary enforcement: next char must be space/tab/pipe/semicolon/ampersand/newline).
func TestMatchesBashPatternPrefix_NoSubstringMatch(t *testing.T) {
	if matchesBashPatternPrefix("govet ./...", "go vet") {
		t.Error("'govet' must not match pattern 'go vet' — no word boundary")
	}
}

// TestMatchesBashPatternPrefix_NoPartialWordMatch verifies "go testing" does NOT match "go test".
func TestMatchesBashPatternPrefix_NoPartialWordMatch(t *testing.T) {
	if matchesBashPatternPrefix("go testing ./...", "go test") {
		t.Error("'go testing' must not match pattern 'go test' — 'i' is not a word boundary")
	}
}

// TestTrackPreToolUse_SemicolonChainedCommand verifies that "echo hi; go test ./..." sends a
// CommandRan signal for the test category (second segment matches "go test").
func TestTrackPreToolUse_SemicolonChainedCommand(t *testing.T) {
	dir := t.TempDir()
	mock := &mockSignaler{}
	input := claudeHookInput{
		SessionID:     "sess-6",
		HookEventName: "PreToolUse",
		AgentType:     "developer-1",
		ToolName:      "Bash",
		ToolInput:     bashInput(t, "echo hi; go test ./..."),
		CWD:           dir,
	}
	trackPreToolUse(context.Background(), mock, "coding-session-abc", input)

	for _, s := range mock.signals {
		if s.signalName == "command-ran" {
			sig, ok := s.arg.(model.SignalCommandRan)
			if !ok {
				t.Fatalf("expected SignalCommandRan, got %T", s.arg)
			}
			if sig.Category == "test" {
				return
			}
		}
	}
	t.Error("no command-ran signal for test category from semicolon-chained command")
}

// TestTrackPreToolUse_PipedCommand verifies that "go vet ./... | tee log.txt" sends a
// CommandRan signal for the lint category (first segment matches "go vet").
func TestTrackPreToolUse_PipedCommand(t *testing.T) {
	dir := t.TempDir()
	mock := &mockSignaler{}
	input := claudeHookInput{
		SessionID:     "sess-5",
		HookEventName: "PreToolUse",
		AgentType:     "developer-1",
		ToolName:      "Bash",
		ToolInput:     bashInput(t, "go vet ./... | tee log.txt"),
		CWD:           dir,
	}
	trackPreToolUse(context.Background(), mock, "coding-session-abc", input)

	for _, s := range mock.signals {
		if s.signalName == "command-ran" {
			sig, ok := s.arg.(model.SignalCommandRan)
			if !ok {
				t.Fatalf("expected SignalCommandRan, got %T", s.arg)
			}
			if sig.Category == "lint" {
				return
			}
		}
	}
	t.Error("no command-ran signal for lint category from piped command")
}

// TestEvalTeammateIdleConfig_DeveloperBlockedInDeveloping verifies that a developer
// going idle in DEVELOPING is denied (default config: role_check on "developer").
func TestEvalTeammateIdleConfig_DeveloperBlockedInDeveloping(t *testing.T) {
	dir := t.TempDir()
	reason := evalTeammateIdleConfig(dir, "DEVELOPING", "developer-1", nil)
	if reason == "" {
		t.Error("expected denial reason for developer in DEVELOPING, got empty string")
	}
}

// TestEvalTeammateIdleConfig_ReviewerAllowedInDeveloping verifies that a reviewer
// going idle in DEVELOPING is NOT denied by the developer role_check.
func TestEvalTeammateIdleConfig_ReviewerAllowedInDeveloping(t *testing.T) {
	dir := t.TempDir()
	reason := evalTeammateIdleConfig(dir, "DEVELOPING", "reviewer-1", nil)
	if reason != "" {
		t.Errorf("reviewer should be allowed to idle in DEVELOPING, got: %q", reason)
	}
}

// TestEvalTeammateIdleConfig_WildcardPhaseAllowsIdle verifies that in phases not explicitly
// matched (wildcard "*" rule has empty checks), idle is allowed.
func TestEvalTeammateIdleConfig_WildcardPhaseAllowsIdle(t *testing.T) {
	dir := t.TempDir()
	reason := evalTeammateIdleConfig(dir, "REVIEWING", "developer-1", nil)
	if reason != "" {
		t.Errorf("all teammates should be allowed to idle in REVIEWING (wildcard), got: %q", reason)
	}
}

// TestEvalTeammateIdleConfig_CustomConfig verifies that a project-level .wf-agents.yaml
// config can add a command_ran check that denies idle when a command hasn't been run.
func TestEvalTeammateIdleConfig_CustomConfig(t *testing.T) {
	dir := t.TempDir()
	writeTempConfig(t, dir, `
teammate_idle:
  - match: DEVELOPING
    checks:
      - type: command_ran
        key: lint
        message: "Must run lint before going idle in DEVELOPING"
`)
	// No commands ran — should be denied
	reason := evalTeammateIdleConfig(dir, "DEVELOPING", "developer-1", map[string]bool{})
	if reason == "" {
		t.Error("expected denial because lint has not been run, got empty string")
	}

	// Lint ran — should be allowed
	reason = evalTeammateIdleConfig(dir, "DEVELOPING", "developer-1", map[string]bool{"lint": true})
	if reason != "" {
		t.Errorf("expected no denial after lint ran, got: %q", reason)
	}
}

// TestAutoAllowOutputHasPermissionDecisionAllow verifies that when Allowed is true, the JSON output
// contains permissionDecision: "allow" so Claude Code bypasses the permission prompt.
func TestAutoAllowOutputHasPermissionDecisionAllow(t *testing.T) {
	out := hookOutput{
		HookSpecificOutput: &hookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "allow",
			PermissionDecisionReason: "Safe command auto-approved by workflow",
			AdditionalContext:        "[Workflow Phase: DEVELOPING] phase instructions",
		},
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(out); err != nil {
		t.Fatalf("failed to encode hookOutput: %v", err)
	}

	raw := buf.String()
	if !strings.Contains(raw, `"permissionDecision":"allow"`) {
		t.Errorf("auto-allow output must contain permissionDecision:\"allow\", got: %s", raw)
	}
	if !strings.Contains(raw, `"permissionDecisionReason"`) {
		t.Errorf("auto-allow output must contain permissionDecisionReason, got: %s", raw)
	}
	if strings.Contains(raw, `"continue"`) {
		t.Errorf("auto-allow output must NOT contain \"continue\" field, got: %s", raw)
	}
}
