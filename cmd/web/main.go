package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
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
	MRUrl         string `json:"mr_url,omitempty"`
	ProjectName   string `json:"project_name,omitempty"`
	RepoURL       string `json:"repo_url,omitempty"`
	StartTime     string `json:"start_time"`
	LastUpdatedAt     string `json:"last_updated_at,omitempty"`
	ActiveAgentsCount int    `json:"active_agents_count,omitempty"`
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
		switch wfe.Status {
		case enums.WORKFLOW_EXECUTION_STATUS_COMPLETED:
			status = "COMPLETED"
		case enums.WORKFLOW_EXECUTION_STATUS_FAILED:
			status = "FAILED"
		case enums.WORKFLOW_EXECUTION_STATUS_TERMINATED:
			status = "TERMINATED"
		case enums.WORKFLOW_EXECUTION_STATUS_CANCELED:
			status = "CANCELED"
		}

		// Extract task, mr_url, project_name, and repo_url from memo
		task := ""
		mrUrl := ""
		projectName := ""
		repoURL := ""
		if wfe.Memo != nil {
			if payload, ok := wfe.Memo.Fields["task"]; ok {
				var t string
				if json.Unmarshal(payload.Data, &t) == nil {
					task = t
				}
			}
			if payload, ok := wfe.Memo.Fields["mr_url"]; ok {
				var u string
				if json.Unmarshal(payload.Data, &u) == nil {
					mrUrl = u
				}
			}
			if payload, ok := wfe.Memo.Fields["project_name"]; ok {
				var p string
				if json.Unmarshal(payload.Data, &p) == nil {
					projectName = p
				}
			}
			if payload, ok := wfe.Memo.Fields["repo_url"]; ok {
				var p string
				if json.Unmarshal(payload.Data, &p) == nil {
					repoURL = p
				}
			}
		}

		item := workflowListItem{
			WorkflowID:  wfID,
			RunID:       wfe.Execution.RunId,
			SessionID:   sessionID,
			Status:      status,
			Task:        task,
			MRUrl:       mrUrl,
			ProjectName: projectName,
			RepoURL:     repoURL,
			StartTime:   startTime,
		}

		// Query current phase, last update time, and mr_url for running workflows
		if status == "RUNNING" {
			qctx, qcancel := context.WithTimeout(ctx, 2*time.Second)
			resp, err := s.temporal.QueryWorkflow(qctx, wfID, "", wf.QueryStatus)
			qcancel()
			if err == nil {
				var wfStatus model.WorkflowStatus
				if resp.Get(&wfStatus) == nil {
					item.Phase = string(wfStatus.Phase)
					item.LastUpdatedAt = wfStatus.LastUpdatedAt
					item.ActiveAgentsCount = len(wfStatus.ActiveAgents)
					if wfStatus.MRUrl != "" {
						item.MRUrl = wfStatus.MRUrl
					}
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

	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}

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
		sessionID := strings.TrimPrefix(workflowID, "coding-session-")
		writeJSON(w, workflowStatusResponse{WorkflowStatus: status, ChannelAvailable: channelAvailable(sessionID)})

	case "timeline":
		afterStr := r.URL.Query().Get("after")
		var resp interface{ Get(interface{}) error }
		var err error
		if afterStr != "" {
			after, parseErr := strconv.Atoi(afterStr)
			if parseErr == nil {
				resp, err = s.temporal.QueryWorkflow(ctx, workflowID, runID, wf.QueryTimelineIncremental, after)
			}
		}
		if resp == nil {
			// No valid "after" param — use recent (bounded) query with fallback to full timeline
			limit := 500
			resp, err = s.temporal.QueryWorkflow(ctx, workflowID, runID, wf.QueryTimelineRecent, limit)
			if err != nil {
				// Fall back to full timeline for older workflows that don't have the new query
				resp, err = s.temporal.QueryWorkflow(ctx, workflowID, runID, wf.QueryTimeline)
				if err != nil {
					http.Error(w, fmt.Sprintf("Query failed: %v", err), http.StatusInternalServerError)
					return
				}
			}
		} else if err != nil {
			// QueryTimelineIncremental failed — fall back to QueryTimelineRecent then QueryTimeline
			limit := 500
			resp, err = s.temporal.QueryWorkflow(ctx, workflowID, runID, wf.QueryTimelineRecent, limit)
			if err != nil {
				resp, err = s.temporal.QueryWorkflow(ctx, workflowID, runID, wf.QueryTimeline)
				if err != nil {
					http.Error(w, fmt.Sprintf("Query failed: %v", err), http.StatusInternalServerError)
					return
				}
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

	case "message":
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Message == "" {
			http.Error(w, "message field required", http.StatusBadRequest)
			return
		}
		sessionID := strings.TrimPrefix(workflowID, "coding-session-")
		err := s.temporal.SignalWorkflow(ctx, workflowID, runID, wf.SignalJournal, model.SignalJournal{
			SessionID: "web-dashboard",
			Message:   body.Message,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to send message: %v", err), http.StatusInternalServerError)
			return
		}
		channelDelivered := forwardToChannel(sessionID, body.Message)
		writeJSON(w, map[string]interface{}{"status": "sent", "channel_delivered": channelDelivered})

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
	Phases             *config.PhasesConfig                 `json:"phases"`
	Transitions        map[string][]config.TransitionConfig `json:"transitions"`
	RequiredCategories map[string][]string                  `json:"required_categories,omitempty"`
}

// requiredCategoriesFromConfig builds a map of phase → sorted unique command categories
// required before a teammate can go idle (type: command_ran idle checks).
func requiredCategoriesFromConfig(cfg *config.Config) map[string][]string {
	result := map[string][]string{}
	seen := map[string]map[string]bool{}
	if cfg.Phases == nil {
		return nil
	}
	for phaseName, phaseConfig := range cfg.Phases.Phases {
		for _, rule := range phaseConfig.Idle {
			for _, check := range rule.Checks {
				if check.Type != "command_ran" || check.Category == "" {
					continue
				}
				if seen[phaseName] == nil {
					seen[phaseName] = map[string]bool{}
				}
				if !seen[phaseName][check.Category] {
					seen[phaseName][check.Category] = true
					result[phaseName] = append(result[phaseName], check.Category)
				}
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
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
	var requiredCats map[string][]string
	if defaultCfg, err := config.DefaultConfig(); err == nil {
		requiredCats = requiredCategoriesFromConfig(defaultCfg)
	}
	return workflowConfigResponse{Phases: phases, Transitions: transitions, RequiredCategories: requiredCats}
}

// handleWorkflowConfig returns the phases and transitions from the embedded default config.
func handleWorkflowConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load config: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, workflowConfigResponse{
		Phases:             cfg.Phases,
		Transitions:        cfg.Transitions,
		RequiredCategories: requiredCategoriesFromConfig(cfg),
	})
}

func channelPortFile(sessionID string) string {
	return os.TempDir() + "/wf-agents-channel-ports/" + sessionID + ".json"
}

func channelAvailable(sessionID string) bool {
	_, err := os.Stat(channelPortFile(sessionID))
	return err == nil
}

type workflowStatusResponse struct {
	model.WorkflowStatus
	ChannelAvailable bool `json:"channel_available"`
}

// forwardToChannel POSTs a message to the Claude Code channel for the given session.
// Returns true if delivered, false if channel is unavailable or any error occurs.
func forwardToChannel(sessionID, message string) bool {
	data, err := os.ReadFile(channelPortFile(sessionID))
	if err != nil {
		return false
	}
	var portInfo struct {
		Port int `json:"port"`
	}
	if err := json.Unmarshal(data, &portInfo); err != nil || portInfo.Port == 0 {
		return false
	}
	payload, err := json.Marshal(map[string]string{"message": message})
	if err != nil {
		return false
	}
	httpClient := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/message", portInfo.Port)
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(v)
}

func resolveWorkflowID(id string) string {
	return session.ResolveWorkflowID(id)
}

func temporalHost() string {
	return internaltemporal.Host()
}
