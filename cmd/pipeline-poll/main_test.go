package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eklemin/wf-agents/internal/platform"
)


// pipelineJSON builds a minimal pipeline API response.
func pipelineJSON(id int, status, ref, webURL, createdAt string, duration int) string {
	return fmt.Sprintf(`[{"id":%d,"status":"%s","ref":"%s","web_url":"%s","created_at":"%s","duration":%d}]`,
		id, status, ref, webURL, createdAt, duration)
}

// jobJSON builds a minimal jobs API response.
func jobsJSON(jobs []map[string]interface{}) string {
	data, _ := json.Marshal(jobs)
	return string(data)
}

func makeJob(id int, name, stage, status string, allowFailure bool) map[string]interface{} {
	return map[string]interface{}{
		"id":            id,
		"name":          name,
		"stage":         stage,
		"status":        status,
		"allow_failure": allowFailure,
		"web_url":       fmt.Sprintf("https://gitlab.example.com/jobs/%d", id),
	}
}

// buildRunner returns a mock runner for common pipeline scenarios.
func buildScenarioRunner(branch string, pipelineResp string, jobsResp string, traceResp map[int]string) platform.CmdRunner {
	return func(timeout time.Duration, name string, args ...string) (string, error) {
		if name == "git" {
			return branch + "\n", nil
		}
		if name == "glab" && len(args) > 0 && args[0] == "api" {
			path := ""
			if len(args) > 1 {
				path = args[1]
			}
			// jobs trace endpoint: projects/:id/jobs/{id}/trace
			for jobID, trace := range traceResp {
				if containsStr(path, fmt.Sprintf("/jobs/%d/trace", jobID)) {
					return trace, nil
				}
			}
			// jobs list endpoint
			if containsStr(path, "/jobs") {
				return jobsResp, nil
			}
			// pipeline list endpoint
			if containsStr(path, "/pipelines") {
				return pipelineResp, nil
			}
		}
		return "", fmt.Errorf("unexpected: %s %v", name, args)
	}
}

func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestPipelineRunning(t *testing.T) {
	jobs := jobsJSON([]map[string]interface{}{
		makeJob(1, "build", "build", "running", false),
		makeJob(2, "test", "test", "created", false),
	})
	pipeline := pipelineJSON(100, "running", "main", "https://gitlab.example.com/pipelines/100", "2026-03-22T12:00:00Z", 0)
	runner := buildScenarioRunner("main", pipeline, jobs, nil)

	out, err := buildOutput(runner, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "running" {
		t.Errorf("want running, got %q", out.Status)
	}
	if out.AllJobsDone {
		t.Error("want AllJobsDone=false, got true")
	}
}

func TestPipelineSuccess(t *testing.T) {
	jobs := jobsJSON([]map[string]interface{}{
		makeJob(1, "build", "build", "success", false),
		makeJob(2, "test", "test", "success", false),
	})
	pipeline := pipelineJSON(101, "success", "main", "https://gitlab.example.com/pipelines/101", "2026-03-22T12:00:00Z", 120)
	runner := buildScenarioRunner("main", pipeline, jobs, nil)

	out, err := buildOutput(runner, 101)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "success" {
		t.Errorf("want success, got %q", out.Status)
	}
	if !out.AllJobsDone {
		t.Error("want AllJobsDone=true")
	}
	if len(out.FailedJobs) != 0 {
		t.Errorf("want 0 failed jobs, got %d", len(out.FailedJobs))
	}
}

func TestPipelineOnlyAllowFailureJobs(t *testing.T) {
	jobs := jobsJSON([]map[string]interface{}{
		makeJob(1, "lint", "checks", "success", false),
		makeJob(2, "optional-check", "checks", "failed", true),
	})
	pipeline := pipelineJSON(102, "failed", "feature", "https://gitlab.example.com/pipelines/102", "2026-03-22T12:00:00Z", 60)
	runner := buildScenarioRunner("feature", pipeline, jobs, nil)

	out, err := buildOutput(runner, 102)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "success" {
		t.Errorf("want success (only allow_failure failed), got %q", out.Status)
	}
	if len(out.FailedJobs) != 0 {
		t.Errorf("want 0 critical failed jobs, got %d", len(out.FailedJobs))
	}
}

func TestPipelineCanceled(t *testing.T) {
	jobs := jobsJSON([]map[string]interface{}{
		makeJob(1, "build", "build", "canceled", false),
	})
	pipeline := pipelineJSON(103, "canceled", "main", "https://gitlab.example.com/pipelines/103", "2026-03-22T12:00:00Z", 30)
	runner := buildScenarioRunner("main", pipeline, jobs, nil)

	out, err := buildOutput(runner, 103)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "canceled" {
		t.Errorf("want canceled, got %q", out.Status)
	}
}

func TestPipelineRealFailure(t *testing.T) {
	traceLog := "line1\nline2\nline3\nline4\nline5\nERROR: compilation failed\nline7\nline8\nline9\nline10\nline11\n"
	jobs := jobsJSON([]map[string]interface{}{
		makeJob(1, "build", "build", "success", false),
		makeJob(2, "isolation-tests", "test", "failed", false),
	})
	pipeline := pipelineJSON(104, "failed", "main", "https://gitlab.example.com/pipelines/104", "2026-03-22T12:00:00Z", 90)
	runner := buildScenarioRunner("main", pipeline, jobs, map[int]string{2: traceLog})

	out, err := buildOutput(runner, 104)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "failed" {
		t.Errorf("want failed, got %q", out.Status)
	}
	if len(out.FailedJobs) != 1 {
		t.Fatalf("want 1 failed job, got %d", len(out.FailedJobs))
	}
	fj := out.FailedJobs[0]
	if fj.Name != "isolation-tests" {
		t.Errorf("want job name isolation-tests, got %q", fj.Name)
	}
	if fj.AllowFailure {
		t.Error("want allow_failure=false")
	}
	if len(fj.FailRootCauses) == 0 {
		t.Error("want at least one root cause")
	}
	if fj.LogTail == "" {
		t.Error("want non-empty log_tail")
	}
}

func TestGetMRHeadPipeline(t *testing.T) {
	runner := func(timeout time.Duration, name string, args ...string) (string, error) {
		if name == "glab" && len(args) > 1 && strings.Contains(args[1], "merge_requests/50") {
			return `{"id":50,"source_branch":"feat","head_pipeline":{"id":999,"status":"success","ref":"refs/merge-requests/50/head","web_url":"https://gitlab.example.com/pipelines/999","created_at":"2026-03-22T12:00:00Z","duration":120}}`, nil
		}
		return "", fmt.Errorf("unexpected: %s %v", name, args)
	}

	p, err := getMRHeadPipeline(runner, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID != 999 {
		t.Errorf("want pipeline ID 999, got %d", p.ID)
	}
	if p.Status != "success" {
		t.Errorf("want status success, got %q", p.Status)
	}
}

func TestGetMRHeadPipeline_NoPipeline(t *testing.T) {
	runner := func(timeout time.Duration, name string, args ...string) (string, error) {
		if name == "glab" {
			return `{"id":51,"source_branch":"feat","head_pipeline":null}`, nil
		}
		return "", fmt.Errorf("unexpected: %s %v", name, args)
	}

	_, err := getMRHeadPipeline(runner, 51)
	if err == nil {
		t.Fatal("want error for null head_pipeline, got nil")
	}
}

func TestNoPipelineFound(t *testing.T) {
	runner := func(timeout time.Duration, name string, args ...string) (string, error) {
		if name == "git" {
			return "main\n", nil
		}
		if name == "glab" {
			return "[]", nil
		}
		return "", fmt.Errorf("unexpected: %s %v", name, args)
	}

	_, err := getLatestPipeline(runner, "main")
	if err == nil {
		t.Fatal("want error for no pipeline found, got nil")
	}
}

func TestGlabAPIError(t *testing.T) {
	runner := func(timeout time.Duration, name string, args ...string) (string, error) {
		if name == "git" {
			return "main\n", nil
		}
		if name == "glab" {
			return "", fmt.Errorf("glab: request failed")
		}
		return "", fmt.Errorf("unexpected: %s %v", name, args)
	}

	_, err := getLatestPipeline(runner, "main")
	if err == nil {
		t.Fatal("want error on glab API failure, got nil")
	}
}

func TestStripANSI(t *testing.T) {
	input := "\x1b[31mERROR\x1b[0m: something failed\n\x1b[32mOK\x1b[0m"
	got := stripANSI(input)
	want := "ERROR: something failed\nOK"
	if got != want {
		t.Errorf("stripANSI:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestExtractRootCauses_Basic(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	// Insert an error at line 10 (index 9)
	lines[9] = "ERROR: build failed"

	log := ""
	for _, l := range lines {
		log += l + "\n"
	}

	causes := extractRootCauses(log)
	if len(causes) == 0 {
		t.Fatal("want at least one root cause")
	}
	// The block should contain the error line
	found := false
	for _, c := range causes {
		if containsStr(c, "ERROR: build failed") {
			found = true
		}
	}
	if !found {
		t.Error("expected error line in root causes")
	}
}

func TestExtractRootCauses_MergeOverlapping(t *testing.T) {
	// Two errors close together should merge into one block
	lines := []string{
		"line1", "line2", "line3", "line4", "line5",
		"ERROR: first failure",
		"line7", "line8", "line9", "line10", "line11",
		"FATAL: second failure",
		"line13", "line14", "line15", "line16", "line17",
	}
	log := ""
	for _, l := range lines {
		log += l + "\n"
	}

	causes := extractRootCauses(log)
	// Two errors within 11 lines of each other (5 context each) should merge
	if len(causes) != 1 {
		t.Errorf("want 1 merged block, got %d", len(causes))
	}
}

func TestExtractRootCauses_NoMatches(t *testing.T) {
	log := "everything is fine\nall tests passed\nbuild successful\n"
	causes := extractRootCauses(log)
	if len(causes) != 0 {
		t.Errorf("want 0 causes, got %d", len(causes))
	}
}

func TestExtractRootCauses_IgnoresCIVariableNames(t *testing.T) {
	// CI variable names containing FAIL/ERROR substrings should NOT match.
	log := "FF_ENABLE_BASH_EXIT_CODE_CHECK=true\nSLACK_NOTIFICATIONS_JOB_STATUS_ICON_FAILED=x\nTRIVY_FAIL_ON_VULN=false\n"
	causes := extractRootCauses(log)
	if len(causes) != 0 {
		t.Errorf("want 0 causes (CI var names are not errors), got %d: %v", len(causes), causes)
	}
}

func TestExtractStepScript(t *testing.T) {
	log := "Preparing environment\n\rsection_end:1234:prepare_script\r\rsection_start:1234:step_script\r" +
		"$ go test ./...\n=== RUN   TestFoo\n--- FAIL: TestFoo (0.01s)\nFAIL\n" +
		"\rsection_end:1234:step_script\r\rsection_start:1234:after_script\r" +
		"Running after script...\nSession variables [CI,HELM_ARGS]\nUploading artifacts...\n"

	extracted := extractStepScript(log)
	if strings.Contains(extracted, "Preparing environment") {
		t.Error("should not contain prepare_script content")
	}
	if strings.Contains(extracted, "Session variables") {
		t.Error("should not contain after_script content")
	}
	if strings.Contains(extracted, "Uploading artifacts") {
		t.Error("should not contain after_script content")
	}
	if !strings.Contains(extracted, "--- FAIL: TestFoo") {
		t.Error("should keep test failure lines from step_script")
	}
	if !strings.Contains(extracted, "go test") {
		t.Error("should keep user commands from step_script")
	}
}

func TestExtractStepScript_NoMarkers(t *testing.T) {
	log := "some output\nERROR: build failed\nmore output"
	extracted := extractStepScript(log)
	if extracted != log {
		t.Errorf("without markers should return full log, got %q", extracted)
	}
}

// TestCompactOutputFromLargeJobLog simulates a real GitLab job trace where
// getJobLogTail returns the last 200 lines — the step_script section_start is
// NOT present but section_end IS. The after_script section contains a massive
// session variables dump, Allure upload, and artifact upload noise.
// Verifies that the pipeline produces compact, useful fail_root_causes.
func TestCompactOutputFromLargeJobLog(t *testing.T) {
	// Build a realistic job log tail (no section_start:step_script — it was earlier in the log).
	stepScriptTail := strings.Join([]string{
		"ok  \tgitlab.example.com/pkg/api\t12.5s",
		"ok  \tgitlab.example.com/pkg/service\t8.3s",
		"",
		"=== Failed",
		"=== FAIL: test/cases/dependecies/kafka TestKafkaDepSuite/TestPublishIncidentDecisionEvent (1.39s)",
		"    client_test.go:35:",
		"            Error Trace:\t/builds/project/test/cases/dependecies/kafka/client_test.go:35",
		"            Error:      \tReceived unexpected error:",
		"                        \t[6] Not Leader For Partition: the client attempted to send messages to a replica that is not the leader",
		"            Test:       \tTestKafkaDepSuite/TestPublishIncidentDecisionEvent",
		"",
		"=== FAIL: test/cases/dependecies/kafka TestKafkaDepSuite (1.40s)",
		"    suite.go:64: Using db: test_f61130af38b5c8ef6f2245beec1d5103",
		"",
		"DONE 200 tests, 2 failures in 38.580s",
		"total:\t\t\t\t\t\t\t\t\t\t\t(statements)\t\t\t\t\t\t49.6%",
		"",
		"WARNING: Event retrieved from the cluster: policy disallow-external-images fail: validation failure",
	}, "\n")

	afterScript := strings.Join([]string{
		"Running after_script",
		"Running after script...",
		"$ echo \"--> Upload test results (will run even on failure)...\"",
		"--> Upload test results (will run even on failure)...",
		"Allure TestOps Client v2.18.0",
		"User mariia.pastushenkova",
		"Project id [16] name [iRiski] public",
		"Job uid [1173], name [reshalo]",
		"Job Run uid [1478994], name [1478994]",
		"Job Run params [Branch=RISKDEV-11359_remove-token-from-logs]",
		"Launch [423565] name [reshalo - 3bf4b1fe]",
		// Simulate the massive session variables dump
		"Session variables [FF_USE_WINDOWS_LEGACY_PROCESS_STRATEGY,CONFIG_QUALITY_GATE," +
			"FF_SKIP_NOOP_BUILD_STAGES,CI_COMMIT_REF_PROTECTED,TEST_POSTGRES_MIGRATIONS_PATH," +
			"CI_CONCURRENT_PROJECT_ID,CYPROC_VAULT_SERVER_URL_PROD,CI_PROJECT_CLASSIFICATION_LABEL," +
			"TEST_REDIS_ADDRESSES,TEST_MONGO_PASSWORD,CI_PIPELINE_URL,KUBERNETES_PORT_443_TCP_PORT," +
			"FF_ENABLE_BASH_EXIT_CODE_CHECK,VAULT_SERVER_URL_PROD,ALLURE_CI_TYPE," +
			"TRIVY_FAIL_ON_VULN,SLACK_NOTIFICATIONS_JOB_STATUS_ICON_FAILED," +
			"HOME,CI_DEFAULT_BRANCH,PATH,TRIVY_VERSION,VAULT_AUTH_ROLE]",
		"Session [456057] started",
		"Report link: https://allure.beta.diftech.org/jobrun/478451",
		"Watcher is waiting for indexing complete...",
		"Total files indexed: 211 || Finished files: 211 || Orphan Files: 0",
		"Waiting Finished. Waited for 2.002976661s",
		"Watcher finished in [4.004396769s]",
		"Session [456057] finished",
		"Job Run [478451] stopped",
		"Report link: https://allure.beta.diftech.org/jobrun/478451",
		"",
		"\rsection_end:1774481982:after_script\r",
		"\rsection_start:1774481982:upload_artifacts_on_failure\r",
		"Uploading artifacts for failed job",
		"Uploading artifacts...",
		"reports/isolation_report.xml: found 1 matching artifact files and directories",
		`Uploading artifacts as "junit" to coordinator... 201 Created`,
		"",
		"\rsection_end:1774481984:upload_artifacts_on_failure\r",
		"\rsection_start:1774481984:cleanup_file_variables\r",
		"Cleaning up project directory and file based variables",
		"\rsection_end:1774481984:cleanup_file_variables\r",
		"ERROR: Job failed: command terminated with exit code 1",
	}, "\n")

	rawLog := stepScriptTail +
		"\n\rsection_end:1774481978:step_script\r" +
		"\rsection_start:1774481978:after_script\r" +
		afterScript

	// Step 1: extractStepScript should strip after_script even without section_start
	cleaned := extractStepScript(rawLog)
	if strings.Contains(cleaned, "Session variables") {
		t.Error("extractStepScript should strip after_script session variables dump")
	}
	if strings.Contains(cleaned, "Uploading artifacts") {
		t.Error("extractStepScript should strip artifact upload noise")
	}
	if strings.Contains(cleaned, "Allure TestOps") {
		t.Error("extractStepScript should strip Allure upload output")
	}
	if !strings.Contains(cleaned, "Not Leader For Partition") {
		t.Error("extractStepScript should preserve the test error message")
	}

	// Step 2: extractGoTestFailures should return compact summary, stopping at DONE
	causes := extractGoTestFailures(cleaned)
	if len(causes) == 0 {
		t.Fatal("want at least one Go test failure block")
	}
	cause := causes[0]
	if !strings.Contains(cause, "=== Failed") {
		t.Error("cause should start with '=== Failed'")
	}
	if !strings.Contains(cause, "Not Leader For Partition") {
		t.Error("cause should contain the actual error")
	}
	if !strings.Contains(cause, "DONE 200 tests") {
		t.Error("cause should include the DONE summary line")
	}
	// Must NOT contain anything after DONE
	if strings.Contains(cause, "total:") {
		t.Error("cause should stop at DONE line, not include coverage stats")
	}
	if strings.Contains(cause, "WARNING:") {
		t.Error("cause should not include policy warnings after DONE")
	}

	// Verify compactness: the cause block should be well under 1KB
	if len(cause) > 1024 {
		t.Errorf("cause should be compact, got %d bytes:\n%s", len(cause), cause)
	}
}

// TestCompactOutputFromTrivyScanFailure simulates a docker-build job that failed
// due to Trivy finding critical vulnerabilities. The log contains docker buildx noise,
// Trivy scan tables (repeated per binary), quality gate block, and DefectDojo link.
// Verifies that extractTrivyFindings produces compact, actionable root causes.
func TestCompactOutputFromTrivyScanFailure(t *testing.T) {
	// Simulate the tail of a docker-build job log (what getJobLogTail would return).
	// Includes: end of docker build, Trivy scan, quality gate, cleanup.
	trivyJobLog := strings.Join([]string{
		// Docker build tail (noise)
		"#18 pushing manifest for 934587711440.dkr.ecr.eu-central-1.amazonaws.com/iriski/reshalo:tag@sha256:5ee9f5",
		"#18 DONE 1.0s",
		"#19 exporting cache to registry",
		"#19 writing layer sha256:0e2882870326d12f2187dd62331537510b301d4cff1fd48b13312c6542c5d372 0.0s done",
		"#19 writing layer sha256:29896d3ad0efa320eaaffe6d662651e264a7c20cdb828fdde11d37e2e0de63e1 4.0s done",
		"#19 DONE 24.2s",
		"real    0m51.617s",
		"user    0m1.326s",
		"sys    0m0.498s",
		"Run",
		"Image size: 86.29M",
		"",
		// Trivy scan report — first binary
		"app/api (gobinary)",
		"==================",
		"Total: 1 (CRITICAL: 1)",
		"┌────────────────────────┬────────────────┬──────────┬────────┬───────────────────┬───────────────┬─────────────────────────────────┐",
		"│        Library         │ Vulnerability  │ Severity │ Status │ Installed Version │ Fixed Version │              Title              │",
		"├────────────────────────┼────────────────┼──────────┼────────┼───────────────────┼───────────────┼─────────────────────────────────┤",
		"│ google.golang.org/grpc │ CVE-2026-33186 │ CRITICAL │ fixed  │ v1.75.1           │ 1.79.3        │ gRPC-Go authorization bypass    │",
		"│                        │                │          │        │                   │               │ https://avd.aquasec.com/nvd/cve-2026-33186 │",
		"└────────────────────────┴────────────────┴──────────┴────────┴───────────────────┴───────────────┴─────────────────────────────────┘",
		"",
		// Second binary (same CVE — should NOT be duplicated in output)
		"app/app (gobinary)",
		"==================",
		"Total: 1 (CRITICAL: 1)",
		"┌────────────────────────┬────────────────┬──────────┬────────┬───────────────────┬───────────────┬─────────────────────────────────┐",
		"│        Library         │ Vulnerability  │ Severity │ Status │ Installed Version │ Fixed Version │              Title              │",
		"├────────────────────────┼────────────────┼──────────┼────────┼───────────────────┼───────────────┼─────────────────────────────────┤",
		"│ google.golang.org/grpc │ CVE-2026-33186 │ CRITICAL │ fixed  │ v1.75.1           │ 1.79.3        │ gRPC-Go authorization bypass    │",
		"│                        │                │          │        │                   │               │ https://avd.aquasec.com/nvd/cve-2026-33186 │",
		"└────────────────────────┴────────────────┴──────────┴────────┴───────────────────┴───────────────┴─────────────────────────────────┘",
		"",
		// Quality gate summary
		"========================================================================================================================",
		"Vulnerability analysis in container images (Containers)",
		"Scaner: trivy",
		"Vulnerabilities found:",
		"critical - 4",
		"high - 0",
		"medium - 0",
		"Container: stage_trivy_934587711440.dkr.ecr.eu-central-1.amazonaws.com/iriski/reshalo",
		"Link for Defect Dojo: https://defectdojo.beta.diftech.org/product/236/finding/open?test__test_type=89&active=true&severity=Critical",
		"========================================================================================================================",
		// Quality gate blockage reason
		"================================================================================================================================",
		"||                                                 Reason for the blockage!!!                                                 ||",
		"||                   There are \"critical\" vulnerabilities in the container. Scanner: \"containers_trivy\" > 0                   ||",
		"||                                            What to Do If You Encounter a Block                                             ||",
		"||                                               1. Follow the DefectDojo Link                                                ||",
		"||                  .  Use the link provided above (Link for Defect Dojo) to see why the build was blocked.                   ||",
		"||                                            2. Fix All Blocking Vulnerabilities                                             ||",
		"||                                   .  Resolve all issues and trigger a new image build.                                     ||",
		"||                             3. No Access to DefectDojo or unable to fix the vulnerabilities?                                ||",
		"||                                              .  Ask for in the Slack channel:                                              ||",
		"||                 .  #infosec-quality-gate https://grid-dif-tech.enterprise.slack.com/archives/C07UCT90VJR                   ||",
		"================================================================================================================================",
		"Cleaning up project directory and file based variables",
		"ERROR: Job failed: command terminated with exit code 1",
	}, "\n")

	// extractStepScript — no section markers in this log tail, returns as-is
	cleaned := extractStepScript(trivyJobLog)

	// extractTrivyFindings should produce a single compact block
	causes := extractTrivyFindings(cleaned)
	if len(causes) != 1 {
		t.Fatalf("want exactly 1 cause block, got %d", len(causes))
	}

	cause := causes[0]

	// Must contain: CVE one-liner with library and versions
	if !strings.Contains(cause, "CVE-2026-33186") {
		t.Error("should contain CVE identifier")
	}
	if !strings.Contains(cause, "google.golang.org/grpc") {
		t.Error("should contain vulnerable library name")
	}
	if !strings.Contains(cause, "1.79.3") {
		t.Error("should contain fixed version")
	}

	// Must contain: DefectDojo link
	if !strings.Contains(cause, "defectdojo.beta.diftech.org") {
		t.Error("should contain DefectDojo link")
	}

	// Must contain: vulnerability counts
	if !strings.Contains(cause, "critical - 4") {
		t.Error("should contain vulnerability count")
	}

	// Must contain: Slack channel
	if !strings.Contains(cause, "#infosec-quality-gate") {
		t.Error("should contain Slack channel for help")
	}

	// Must NOT contain: ASCII table art
	if strings.Contains(cause, "┌") || strings.Contains(cause, "└") || strings.Contains(cause, "├") {
		t.Error("should not contain ASCII table borders")
	}

	// Must NOT contain: docker build noise
	if strings.Contains(cause, "pushing manifest") || strings.Contains(cause, "writing layer") {
		t.Error("should not contain docker build output")
	}

	// CVE must appear exactly once (deduplicated across 4 binaries)
	if strings.Count(cause, "CVE-2026-33186") != 1 {
		t.Errorf("CVE should appear exactly once, got %d", strings.Count(cause, "CVE-2026-33186"))
	}

	// Verify compactness — should be well under 1KB
	if len(cause) > 512 {
		t.Errorf("cause should be very compact, got %d bytes", len(cause))
	}

	t.Logf("Trivy cause (%d bytes):\n%s", len(cause), cause)
}

func TestExtractGoTestFailures_SummaryBlock(t *testing.T) {
	log := `PASS
ok  gitlab.example.com/pkg1 10.5s

=== Failed
=== FAIL: test/cases/kafka TestKafkaDepSuite/TestPublishEvent (0.51s)
    client_test.go:35:
            Error: Not Leader For Partition
            Test: TestKafkaDepSuite/TestPublishEvent

=== FAIL: test/cases/kafka TestKafkaDepSuite (0.51s)

DONE 200 tests, 2 failures in 70.368s`

	causes := extractGoTestFailures(log)
	if len(causes) == 0 {
		t.Fatal("want at least one Go test failure block")
	}
	if !strings.Contains(causes[0], "=== Failed") {
		t.Error("want block starting with '=== Failed'")
	}
	if !strings.Contains(causes[0], "Not Leader For Partition") {
		t.Error("want error message in block")
	}
}

func TestExtractGoTestFailures_NoSummary(t *testing.T) {
	log := `=== RUN   TestFoo
--- FAIL: TestFoo (0.01s)
    foo_test.go:10: expected 1, got 2
FAIL`

	causes := extractGoTestFailures(log)
	if len(causes) == 0 {
		t.Fatal("want at least one Go test failure block")
	}
	if !strings.Contains(causes[0], "--- FAIL: TestFoo") {
		t.Errorf("want test failure line, got %q", causes[0])
	}
}

func TestExtractGoTestFailures_NonePresent(t *testing.T) {
	log := "line1\nline2\nline3\n"
	causes := extractGoTestFailures(log)
	if len(causes) != 0 {
		t.Errorf("want 0, got %d", len(causes))
	}
}

func TestComputeStages(t *testing.T) {
	jobs := []Job{
		{Stage: "build", Status: "success"},
		{Stage: "build", Status: "success"},
		{Stage: "test", Status: "failed"},
		{Stage: "test", Status: "success"},
	}
	stages := computeStages(jobs)

	stageMap := map[string]string{}
	for _, s := range stages {
		stageMap[s.Name] = s.Status
	}

	if stageMap["build"] != "success" {
		t.Errorf("build stage: want success, got %q", stageMap["build"])
	}
	if stageMap["test"] != "failed" {
		t.Errorf("test stage: want failed, got %q", stageMap["test"])
	}
}

func TestRefFlagOverridesBranch(t *testing.T) {
	var requestedPath string
	runner := func(timeout time.Duration, name string, args ...string) (string, error) {
		if name == "git" {
			return "feature-branch\n", nil
		}
		if name == "glab" && len(args) > 1 {
			requestedPath = args[1]
			return pipelineJSON(200, "success", "main", "https://gitlab.example.com/pipelines/200", "2026-03-22T12:00:00Z", 60), nil
		}
		return "", fmt.Errorf("unexpected: %s %v", name, args)
	}

	p, err := getLatestPipeline(runner, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID != 200 {
		t.Errorf("want pipeline ID 200, got %d", p.ID)
	}
	if !strings.Contains(requestedPath, "ref=main") {
		t.Errorf("want ref=main in API path, got %q", requestedPath)
	}
}

func TestOutputJSON_Schema(t *testing.T) {
	out := Output{
		PipelineID:  999,
		PipelineURL: "https://example.com/pipelines/999",
		CreatedAt:   "2026-03-22T12:00:00Z",
		Duration:    120,
		Status:      "success",
		Stages:      []Stage{{Name: "build", Status: "success"}},
		FailedJobs:  []FailedJob{},
		AllJobsDone: true,
	}

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	for _, field := range []string{"pipeline_id", "pipeline_url", "created_at", "duration_seconds", "status", "stages", "failed_jobs", "all_jobs_done"} {
		if _, ok := m[field]; !ok {
			t.Errorf("missing field %q in output JSON", field)
		}
	}
}
