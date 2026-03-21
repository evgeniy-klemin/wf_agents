package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/eklemin/wf-agents/internal/config"
	"github.com/eklemin/wf-agents/internal/model"
	"github.com/eklemin/wf-agents/internal/noplog"
	"github.com/eklemin/wf-agents/internal/session"
	internaltemporal "github.com/eklemin/wf-agents/internal/temporal"
	wf "github.com/eklemin/wf-agents/internal/workflow"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

//go:embed static
var staticFS embed.FS

func main() {
	port := "8090"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	c, err := client.Dial(client.Options{
		HostPort: temporalHost(),
		Logger:   noplog.New(),
	})
	if err != nil {
		log.Fatalf("Failed to connect to Temporal: %v", err)
	}
	defer c.Close()

	srv := &server{temporal: c}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/workflows", srv.handleListWorkflows)
	mux.HandleFunc("/api/terminate/", srv.handleTerminate)
	mux.HandleFunc("/api/workflows/", srv.handleWorkflowDetail)
	mux.HandleFunc("/api/workflow-config", handleWorkflowConfig)
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	log.Printf("Workflow Dashboard starting on http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

type server struct {
	temporal client.Client
}

// workflowListItem is a summary for the workflow list.
type workflowListItem struct {
	WorkflowID    string `json:"workflow_id"`
	RunID         string `json:"run_id"`
	SessionID     string `json:"session_id"`
	Status        string `json:"status"`
	Phase         string `json:"phase,omitempty"`
	Task          string `json:"task,omitempty"`
	StartTime     string `json:"start_time"`
	LastUpdatedAt string `json:"last_updated_at,omitempty"`
}

func (s *server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.temporal.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
		Namespace: "default",
		Query:     `WorkflowType = "CodingSessionWorkflow"`,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list workflows: %v", err), http.StatusInternalServerError)
		return
	}

	items := make([]workflowListItem, 0, len(resp.Executions))
	for _, wfe := range resp.Executions {
		startTime := ""
		if wfe.StartTime != nil {
			startTime = wfe.StartTime.AsTime().Format(time.RFC3339)
		}
		wfID := wfe.Execution.WorkflowId
		sessionID := strings.TrimPrefix(wfID, "coding-session-")

		status := "RUNNING"
		if wfe.Status == enums.WORKFLOW_EXECUTION_STATUS_COMPLETED {
			status = "COMPLETED"
		} else if wfe.Status == enums.WORKFLOW_EXECUTION_STATUS_FAILED {
			status = "FAILED"
		} else if wfe.Status == enums.WORKFLOW_EXECUTION_STATUS_TERMINATED {
			status = "TERMINATED"
		} else if wfe.Status == enums.WORKFLOW_EXECUTION_STATUS_CANCELED {
			status = "CANCELED"
		}

		// Extract task from memo
		task := ""
		if wfe.Memo != nil {
			if payload, ok := wfe.Memo.Fields["task"]; ok {
				var t string
				if json.Unmarshal(payload.Data, &t) == nil {
					task = t
				}
			}
		}

		item := workflowListItem{
			WorkflowID: wfID,
			RunID:      wfe.Execution.RunId,
			SessionID:  sessionID,
			Status:     status,
			Task:       task,
			StartTime:  startTime,
		}

		// Query current phase and last update time for running workflows
		if status == "RUNNING" {
			qctx, qcancel := context.WithTimeout(ctx, 2*time.Second)
			resp, err := s.temporal.QueryWorkflow(qctx, wfID, "", wf.QueryStatus)
			qcancel()
			if err == nil {
				var wfStatus model.WorkflowStatus
				if resp.Get(&wfStatus) == nil {
					item.Phase = string(wfStatus.Phase)
					item.LastUpdatedAt = wfStatus.LastUpdatedAt
				}
			}
		}

		items = append(items, item)
	}

	writeJSON(w, items)
}

func (s *server) handleWorkflowDetail(w http.ResponseWriter, r *http.Request) {
	// Extract workflow ID from path: /api/workflows/{id}/status or /api/workflows/{id}/timeline
	path := strings.TrimPrefix(r.URL.Path, "/api/workflows/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		http.Error(w, "Expected /api/workflows/{id}/{status|timeline}", http.StatusBadRequest)
		return
	}

	workflowID := resolveWorkflowID(parts[0])
	action := parts[1]
	runID := r.URL.Query().Get("run_id")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	switch action {
	case "status":
		resp, err := s.temporal.QueryWorkflow(ctx, workflowID, runID, wf.QueryStatus)
		if err != nil {
			http.Error(w, fmt.Sprintf("Query failed: %v", err), http.StatusInternalServerError)
			return
		}
		var status model.WorkflowStatus
		if err := resp.Get(&status); err != nil {
			http.Error(w, fmt.Sprintf("Decode failed: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, status)

	case "timeline":
		limit := 500
		// Try timeline-recent first (bounded size, avoids Temporal 2MB query limit)
		resp, err := s.temporal.QueryWorkflow(ctx, workflowID, runID, wf.QueryTimelineRecent, limit)
		if err != nil {
			// Fall back to full timeline for older workflows that don't have the new query
			resp, err = s.temporal.QueryWorkflow(ctx, workflowID, runID, wf.QueryTimeline)
			if err != nil {
				http.Error(w, fmt.Sprintf("Query failed: %v", err), http.StatusInternalServerError)
				return
			}
		}
		var timeline model.WorkflowTimeline
		if err := resp.Get(&timeline); err != nil {
			http.Error(w, fmt.Sprintf("Decode failed: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, timeline)

	case "config":
		resp, err := s.temporal.QueryWorkflow(ctx, workflowID, runID, wf.QueryWorkflowConfig)
		if err != nil {
			// Fall back to default config for workflows without a snapshot
			handleWorkflowConfig(w, r)
			return
		}
		var flow model.FlowSnapshot
		if err := resp.Get(&flow); err != nil {
			handleWorkflowConfig(w, r)
			return
		}
		writeJSON(w, flowSnapshotToConfigResponse(&flow))

	default:
		http.Error(w, "Unknown action: "+action, http.StatusBadRequest)
	}
}

func (s *server) handleTerminate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	wfID := resolveWorkflowID(strings.TrimPrefix(r.URL.Path, "/api/terminate/"))
	if wfID == "" {
		http.Error(w, "workflow ID required", http.StatusBadRequest)
		return
	}
	runID := r.URL.Query().Get("run_id")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	err := s.temporal.TerminateWorkflow(ctx, wfID, runID, "terminated via dashboard")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to terminate: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "terminated", "workflow_id": wfID})
}

// workflowConfigResponse is the JSON shape returned by GET /api/workflow-config.
type workflowConfigResponse struct {
	Phases      *config.PhasesConfig                 `json:"phases"`
	Transitions map[string][]config.TransitionConfig `json:"transitions"`
}

// flowSnapshotToConfigResponse converts a FlowSnapshot into the same JSON shape
// as /api/workflow-config so the frontend's applyWorkflowConfig() works unchanged.
func flowSnapshotToConfigResponse(flow *model.FlowSnapshot) workflowConfigResponse {
	phases := &config.PhasesConfig{
		Start:      flow.Start,
		Stop:       flow.Stop,
		PhaseOrder: flow.PhaseOrder,
		Phases:     make(map[string]config.PhaseConfig, len(flow.Phases)),
	}
	for name, fp := range flow.Phases {
		pc := config.PhaseConfig{
			Display: config.PhaseDisplay{
				Label: fp.Display.Label,
				Icon:  fp.Display.Icon,
				Color: fp.Display.Color,
			},
			Instructions: fp.Instructions,
			Hint:         fp.Hint,
		}
		for _, se := range fp.OnEnter {
			pc.OnEnter = append(pc.OnEnter, config.SideEffect{Type: se.Type})
		}
		phases.Phases[name] = pc
	}
	transitions := make(map[string][]config.TransitionConfig, len(flow.Transitions))
	for from, ts := range flow.Transitions {
		for _, t := range ts {
			transitions[from] = append(transitions[from], config.TransitionConfig{
				To:      t.To,
				Label:   t.Label,
				When:    t.When,
				Message: t.Message,
			})
		}
	}
	return workflowConfigResponse{Phases: phases, Transitions: transitions}
}

// handleWorkflowConfig returns the phases and transitions from the embedded default config.
func handleWorkflowConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load config: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, workflowConfigResponse{
		Phases:      cfg.Phases,
		Transitions: cfg.Transitions,
	})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(v)
}

func resolveWorkflowID(id string) string {
	return session.ResolveWorkflowID(id)
}

func temporalHost() string {
	return internaltemporal.Host()
}
