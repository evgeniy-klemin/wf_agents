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

	"github.com/eklemin/wf-agents/internal/model"
	"github.com/eklemin/wf-agents/internal/noplog"
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
		resp, err := s.temporal.QueryWorkflow(ctx, workflowID, runID, wf.QueryTimeline)
		if err != nil {
			http.Error(w, fmt.Sprintf("Query failed: %v", err), http.StatusInternalServerError)
			return
		}
		var timeline model.WorkflowTimeline
		if err := resp.Get(&timeline); err != nil {
			http.Error(w, fmt.Sprintf("Decode failed: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, timeline)

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

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(v)
}

func resolveWorkflowID(id string) string {
	if !strings.HasPrefix(id, "coding-session-") {
		return "coding-session-" + id
	}
	return id
}

func temporalHost() string {
	if h := os.Getenv("TEMPORAL_HOST"); h != "" {
		return h
	}
	return "localhost:7233"
}
