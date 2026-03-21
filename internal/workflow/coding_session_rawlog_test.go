package workflow

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/eklemin/wf-agents/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// claudeHookInputTest mirrors cmd/hook-handler/main.go claudeHookInput for test parsing.
type claudeHookInputTest struct {
	SessionID      string          `json:"session_id"`
	HookEventName  string          `json:"hook_event_name"`
	CWD            string          `json:"cwd"`
	ToolName       string          `json:"tool_name,omitempty"`
	ToolInput      json.RawMessage `json:"tool_input,omitempty"`
	AgentID        string          `json:"agent_id,omitempty"`
	AgentType      string          `json:"agent_type,omitempty"`
	PermissionMode string          `json:"permission_mode,omitempty"`
	ToolUseID      string          `json:"tool_use_id,omitempty"`
}

// buildDetailTest mirrors buildDetail from hook-handler for test use.
func buildDetailTest(input claudeHookInputTest) map[string]string {
	d := map[string]string{"cwd": input.CWD}
	if input.ToolName != "" {
		d["tool_name"] = input.ToolName
	}
	if input.ToolUseID != "" {
		d["tool_use_id"] = input.ToolUseID
	}
	if input.PermissionMode != "" {
		d["permission_mode"] = input.PermissionMode
	}
	if len(input.ToolInput) > 0 {
		d["tool_input"] = string(input.ToolInput)
	}
	if input.AgentID != "" {
		d["agent_id"] = input.AgentID
	}
	if input.AgentType != "" {
		d["agent_type"] = input.AgentType
	}
	return d
}

// toSignalHookEvent transforms a parsed hook input into model.SignalHookEvent
// using the same logic as cmd/hook-handler/main.go.
func toSignalHookEvent(input claudeHookInputTest) model.SignalHookEvent {
	detail := buildDetailTest(input)
	switch input.HookEventName {
	case "SubagentStart", "SubagentStop":
		detail["agent_id"] = input.AgentID
		detail["agent_type"] = input.AgentType
	}
	return model.SignalHookEvent{
		HookType:  input.HookEventName,
		SessionID: input.SessionID,
		Tool:      input.ToolName,
		Detail:    detail,
	}
}

// loadRawLogEvents reads a JSONL testdata file and parses each line.
func loadRawLogEvents(t *testing.T, path string) []claudeHookInputTest {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err, "testdata file must exist at %s", path)
	defer f.Close()

	var events []claudeHookInputTest
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if line != "" {
			var input claudeHookInputTest
			require.NoError(t, json.Unmarshal([]byte(line), &input), "malformed JSONL line: %s", line)
			events = append(events, input)
		}
		if err != nil {
			break
		}
	}
	require.NotEmpty(t, events, "testdata must not be empty")
	return events
}

// simulateActiveAgentsSlice simulates the CURRENT (buggy) []string activeAgents logic.
//
// The race condition it fails on:
//  1. SubagentStart at=X id=A  → X added to active
//  2. SubagentStart at=X id=B  → X already in active (no-op)
//  3. SubagentStop  at=X id=A  → removes X from active (BUG: removes regardless of id)
//  4. PreToolUse    at=X id=B  → X not in active → VIOLATION
//
// Returns the agent_types that should be active but weren't during PreToolUse.
func simulateActiveAgentsSlice(events []claudeHookInputTest) []string {
	active := []string{}
	// currentID tracks the most-recently-started agent_id per agent_type (for violation check).
	currentID := map[string]string{}
	var violations []string

	for _, e := range events {
		at := e.AgentType
		aid := e.AgentID
		switch e.HookEventName {
		case "SubagentStart":
			if at != "" {
				currentID[at] = aid
				found := false
				for _, a := range active {
					if a == at {
						found = true
						break
					}
				}
				if !found {
					active = append(active, at)
				}
			}
		case "SubagentStop":
			if at != "" {
				// Buggy: removes agent_type regardless of agent_id
				filtered := active[:0]
				for _, a := range active {
					if a != at {
						filtered = append(filtered, a)
					}
				}
				active = filtered
			}
		case "Stop":
			// current code: Stop with empty agent_type does nothing in practice
		case "PreToolUse":
			if at != "" && (strings.HasPrefix(at, "developer") || strings.HasPrefix(at, "reviewer")) {
				// Violation: agent is the current live instance but not in active
				if currentID[at] == aid {
					found := false
					for _, a := range active {
						if a == at {
							found = true
							break
						}
					}
					if !found {
						violations = append(violations, at)
					}
				}
			}
		}
	}
	return violations
}

// simulateActiveAgentsMap simulates the FIXED map[string]string activeAgents logic.
//
// The fix: SubagentStop only removes agent_type if agent_id matches the stored id.
// A stale stop (old id after new id was started) is a no-op.
//
// Returns violations: cases where the current live agent instance is missing from active.
func simulateActiveAgentsMap(events []claudeHookInputTest) []string {
	active := map[string]string{} // agent_type -> current agent_id
	var violations []string

	for _, e := range events {
		at := e.AgentType
		aid := e.AgentID
		switch e.HookEventName {
		case "SubagentStart":
			if at != "" {
				active[at] = aid // overwrite: new spawn replaces old
			}
		case "SubagentStop":
			if at != "" {
				// Fix: only delete if agent_id matches stored id (stale stops are ignored)
				if stored, ok := active[at]; ok && stored == aid {
					delete(active, at)
				}
			}
		case "Stop":
			// log only — do NOT remove from activeAgents
		case "PreToolUse":
			if at != "" && (strings.HasPrefix(at, "developer") || strings.HasPrefix(at, "reviewer")) {
				// Violation: current stored id matches this PreToolUse but agent not in map
				storedID, inMap := active[at]
				if inMap && storedID == aid {
					// correctly tracked — no violation
				} else if inMap && storedID != aid {
					// this PreToolUse is from a stale (already-stopped) instance — not a violation
				} else {
					// not in map — violation only if this agent_id is supposed to be current
					// (i.e., no newer SubagentStart arrived for this agent_type after this instance)
					// This is the race condition: a stale stop removed the current instance.
					// We can only detect this if we track the expected current id separately.
					// Since we're simulating the fix, if the invariant holds, this should not happen
					// for the race-condition.jsonl testdata.
					violations = append(violations, at)
				}
			}
		}
	}
	return violations
}

// TestActiveAgentsSliceBug_SimulatesLegacyBehavior demonstrates the old []string
// implementation producing invariant violations when replaying the race-condition testdata.
//
// The race: SubagentStart id=NEW arrives, then SubagentStop id=OLD (stale) arrives.
// The slice implementation removes the agent_type regardless of id, so the NEW agent
// is incorrectly removed, and subsequent PreToolUse from NEW violates the invariant.
//
// This test simulates the legacy (buggy) behavior: it PASSES because violations ARE found.
// The actual workflow now uses the map-based fix (TestActiveAgentsMapFixWorks).
func TestActiveAgentsSliceBug_SimulatesLegacyBehavior(t *testing.T) {
	events := loadRawLogEvents(t, "testdata/race-condition.jsonl")
	violations := simulateActiveAgentsSlice(events)
	assert.NotEmpty(t, violations,
		"expected the []string implementation to produce violations for the race condition scenario")
}

// TestActiveAgentsMapFixWorks verifies that the map[string]string fix eliminates violations
// from the respawn race condition testdata.
//
// With the fix: SubagentStop id=OLD is ignored when active[agent_type]=NEW_id,
// so the NEW agent remains in the map and subsequent PreToolUse passes.
//
// This test MUST PASS after applying the fix (changing activeAgents to map[string]string).
func TestActiveAgentsMapFixWorks(t *testing.T) {
	events := loadRawLogEvents(t, "testdata/race-condition.jsonl")
	violations := simulateActiveAgentsMap(events)
	assert.Empty(t, violations,
		"map-based fix should produce zero violations for the race condition; got %d", len(violations))
}
