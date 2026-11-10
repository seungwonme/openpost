package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

// workflowCall is one request the CLI must send and the answer the fake server
// gives. The fixtures are written as literal wire values, independent of the
// client's own request structs, and are consumed strictly in order, so an extra
// or repeated request (such as a retry after a conflict) fails the test.
type workflowCall struct {
	method string
	path   string
	query  map[string]string
	body   map[string]any
	status int
	reply  string
}

const workflowFixtureJSON = `{"id":"wf-1","workspace_id":"ws-1","name":"Comment to DM","description":"","revision":4,"published_revision":3,"enabled":true,"definition":{"schema":1,"source":{"kind":"comment_received"},"steps":[{"id":"reply","kind":"private_comment_reply","name":"Send DM","inputs":{}}]},"last_checked_at":"2026-11-10T09:00:00Z","created_at":"2026-11-01T09:00:00Z","updated_at":"2026-11-10T09:00:00Z"}`

func workflowServer(t *testing.T, calls []workflowCall) *httptest.Server {
	t.Helper()
	next := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/workspaces" {
			_, _ = w.Write([]byte(`[{"id":"ws-1","name":"Production"}]`))
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer op_cli_test" {
			t.Errorf("authorization = %q", got)
		}
		if next >= len(calls) {
			t.Errorf("unexpected extra request %s %s", r.Method, r.URL.RequestURI())
			http.Error(w, `{"detail":"unexpected request"}`, http.StatusTeapot)
			return
		}
		want := calls[next]
		next++
		if r.Method != want.method || r.URL.Path != want.path {
			t.Errorf("request %d = %s %s, want %s %s", next, r.Method, r.URL.Path, want.method, want.path)
		}
		query := map[string]string{"workspace_id": "ws-1"}
		for key, value := range want.query {
			query[key] = value
		}
		got := map[string]string{}
		for key, values := range r.URL.Query() {
			got[key] = values[0]
		}
		if !reflect.DeepEqual(query, got) {
			t.Errorf("request %d query = %v, want %v", next, got, query)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("request %d body is not JSON: %s", next, raw)
			}
		}
		if !reflect.DeepEqual(want.body, body) {
			t.Errorf("request %d body = %v, want %v", next, body, want.body)
		}
		status := want.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(want.reply))
	}))
	t.Cleanup(func() {
		server.Close()
		if next != len(calls) {
			t.Errorf("sent %d requests, want %d", next, len(calls))
		}
	})
	return server
}

func runWorkflowCommand(t *testing.T, server *httptest.Server, args ...string) (string, error) {
	t.Helper()
	t.Setenv("OPENPOST_CONFIG_DIR", t.TempDir())
	full := append([]string{"--instance", server.URL, "--token", "op_cli_test", "--workspace", "Production"}, args...)
	return executeRootCaptureStdout(t, full...)
}

func TestWorkflowListAndGetShowRevisionsAndState(t *testing.T) {
	server := workflowServer(t, []workflowCall{
		{method: "GET", path: "/api/v1/workflows", reply: `[` + workflowFixtureJSON + `]`},
		{method: "GET", path: "/api/v1/workflows/wf-1", reply: workflowFixtureJSON},
		{method: "GET", path: "/api/v1/workflows/wf-1", reply: workflowFixtureJSON},
	})
	listed, err := runWorkflowCommand(t, server, "workflow", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"wf-1", "Comment to DM", "active", "comment_received", "2026-11-10T09:00:00Z"} {
		if !strings.Contains(listed, want) {
			t.Errorf("list output lacks %q:\n%s", want, listed)
		}
	}
	shown, err := runWorkflowCommand(t, server, "workflow", "get", "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"revision", "4", "published_revision", "3", "step reply", "private_comment_reply"} {
		if !strings.Contains(shown, want) {
			t.Errorf("get output lacks %q:\n%s", want, shown)
		}
	}
	asJSON, err := runWorkflowCommand(t, server, "--json", "workflow", "get", "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(asJSON), &decoded); err != nil {
		t.Fatalf("--json output is not JSON: %v\n%s", err, asJSON)
	}
	definition, _ := decoded["definition"].(map[string]any)
	if decoded["revision"] != float64(4) || definition["schema"] != float64(1) {
		t.Errorf("--json dropped fields: %v", decoded)
	}
}

func TestWorkflowSaveSendsExpectedRevisionAndSurfacesConflictWithoutRetry(t *testing.T) {
	file := t.TempDir() + "/workflow.json"
	definition := `{"schema":1,"source":{"kind":"manual"},"steps":[{"id":"draft","kind":"create_draft","name":"Draft","inputs":{"text":{"reference":"source.title"}}}]}`
	writeFile(t, file, `{"name":"Launch","description":"Manual","definition":`+definition+`}`)
	wantBody := map[string]any{
		"name": "Launch", "description": "Manual", "expected_revision": float64(4),
		"definition": map[string]any{"schema": float64(1), "source": map[string]any{"kind": "manual"}, "steps": []any{
			map[string]any{"id": "draft", "kind": "create_draft", "name": "Draft", "inputs": map[string]any{"text": map[string]any{"reference": "source.title"}}},
		}},
	}
	saved := strings.Replace(workflowFixtureJSON, `"revision":4`, `"revision":5`, 1)
	server := workflowServer(t, []workflowCall{
		{method: "PUT", path: "/api/v1/workflows/wf-1", body: wantBody, reply: saved},
	})
	out, err := runWorkflowCommand(t, server, "workflow", "save", "wf-1", "--file", file, "--expected-revision", "4")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "revision") || !strings.Contains(out, "5") {
		t.Errorf("save output must print the new revision:\n%s", out)
	}

	conflict := workflowServer(t, []workflowCall{
		{method: "PUT", path: "/api/v1/workflows/wf-1", body: wantBody, status: http.StatusConflict, reply: `{"title":"Conflict","status":409,"detail":"workflow changed; reload before saving"}`},
	})
	_, err = runWorkflowCommand(t, conflict, "workflow", "save", "wf-1", "--file", file, "--expected-revision", "4")
	if err == nil {
		t.Fatal("a revision conflict must fail the command")
	}
	for _, want := range []string{"HTTP 409", "workflow changed; reload before saving", "nothing was changed", "openpost workflow get wf-1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("conflict error lacks %q: %v", want, err)
		}
	}
}

func TestWorkflowSaveRequiresTheRevisionTheEditIsBasedOn(t *testing.T) {
	file := t.TempDir() + "/definition.json"
	writeFile(t, file, `{"schema":1,"source":{"kind":"manual"},"steps":[]}`)
	server := workflowServer(t, nil)
	_, err := runWorkflowCommand(t, server, "workflow", "save", "wf-1", "--file", file)
	if err == nil || !strings.Contains(err.Error(), "--expected-revision") {
		t.Fatalf("save without a revision must fail before any request, got %v", err)
	}
}

func TestWorkflowSaveReadsRevisionFromWorkflowGetOutput(t *testing.T) {
	file := t.TempDir() + "/workflow.json"
	writeFile(t, file, workflowFixtureJSON)
	server := workflowServer(t, []workflowCall{
		{method: "PUT", path: "/api/v1/workflows/wf-1", body: map[string]any{
			"name": "Comment to DM", "description": "", "expected_revision": float64(4),
			"definition": map[string]any{"schema": float64(1), "source": map[string]any{"kind": "comment_received"}, "steps": []any{
				map[string]any{"id": "reply", "kind": "private_comment_reply", "name": "Send DM", "inputs": map[string]any{}},
			}},
		}, reply: workflowFixtureJSON},
	})
	if _, err := runWorkflowCommand(t, server, "workflow", "save", "wf-1", "--file", file); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowCreateSendsRevisionZeroAndNeedsAName(t *testing.T) {
	file := t.TempDir() + "/definition.json"
	writeFile(t, file, `{"schema":1,"source":{"kind":"manual"},"steps":[]}`)
	server := workflowServer(t, []workflowCall{
		{method: "POST", path: "/api/v1/workflows", body: map[string]any{
			"name": "Fresh", "description": "", "expected_revision": float64(0),
			"definition": map[string]any{"schema": float64(1), "source": map[string]any{"kind": "manual"}, "steps": []any{}},
		}, reply: strings.Replace(workflowFixtureJSON, `"revision":4`, `"revision":1`, 1)},
	})
	if _, err := runWorkflowCommand(t, server, "workflow", "create", "--file", file, "--name", "Fresh"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWorkflowCommand(t, workflowServer(t, nil), "workflow", "create", "--file", file); err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("create without a name must fail before any request, got %v", err)
	}
}

func TestWorkflowPublishPauseDeleteUseCurrentRevisionAndConfirmation(t *testing.T) {
	revision := map[string]any{"expected_revision": float64(4)}
	server := workflowServer(t, []workflowCall{
		{method: "GET", path: "/api/v1/workflows/wf-1", reply: workflowFixtureJSON},
		{method: "POST", path: "/api/v1/workflows/wf-1/publish", body: revision, reply: strings.Replace(workflowFixtureJSON, `"revision":4`, `"revision":5`, 1)},
		{method: "POST", path: "/api/v1/workflows/wf-1/pause", body: map[string]any{"expected_revision": float64(9)}, reply: workflowFixtureJSON},
		{method: "GET", path: "/api/v1/workflows/wf-1", reply: workflowFixtureJSON},
		{method: "POST", path: "/api/v1/workflows/wf-1/stop-replies", body: revision, reply: workflowFixtureJSON},
		{method: "DELETE", path: "/api/v1/workflows/wf-1", reply: `{"deleted":true}`},
	})
	published, err := runWorkflowCommand(t, server, "--yes", "workflow", "publish", "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(published, "5") {
		t.Errorf("publish must print the new revision:\n%s", published)
	}
	if _, err := runWorkflowCommand(t, server, "workflow", "pause", "wf-1", "--expected-revision", "9"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWorkflowCommand(t, server, "--yes", "workflow", "stop-replies", "wf-1"); err != nil {
		t.Fatal(err)
	}
	deleted, err := runWorkflowCommand(t, server, "--yes", "--json", "workflow", "delete", "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(deleted, `"status": "deleted"`) {
		t.Errorf("delete --json output = %s", deleted)
	}

	// Destructive commands do not reach the server without --yes in JSON mode.
	refusing := workflowServer(t, nil)
	for _, args := range [][]string{
		{"--json", "workflow", "delete", "wf-1"},
		{"--json", "workflow", "publish", "wf-1"},
		{"--json", "workflow", "stop-replies", "wf-1"},
		{"--json", "workflow", "run", "cancel", "run-1"},
		{"--json", "workflow", "run", "approve", "run-1"},
	} {
		if _, err := runWorkflowCommand(t, refusing, args...); err == nil || !strings.Contains(err.Error(), "--yes") {
			t.Errorf("%v must require --yes, got %v", args, err)
		}
	}
}

func TestWorkflowPublishConflictIsAnErrorWithoutRetry(t *testing.T) {
	server := workflowServer(t, []workflowCall{
		{method: "GET", path: "/api/v1/workflows/wf-1", reply: workflowFixtureJSON},
		{method: "POST", path: "/api/v1/workflows/wf-1/publish", body: map[string]any{"expected_revision": float64(4)}, status: http.StatusConflict, reply: `{"detail":"workflow changed; reload before saving"}`},
	})
	_, err := runWorkflowCommand(t, server, "--yes", "workflow", "publish", "wf-1")
	if err == nil || !strings.Contains(err.Error(), "HTTP 409") || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("publish conflict must be an error, got %v", err)
	}
}

func TestWorkflowRunsListStopReasonsAndRunShowsSteps(t *testing.T) {
	reason := "This person reached this workflow's daily limit, so this message did not start it."
	server := workflowServer(t, []workflowCall{
		{method: "GET", path: "/api/v1/workflow-run-summaries", query: map[string]string{"workflow_id": "wf-1"}, reply: `[{"id":"run-1","workflow_id":"wf-1","workspace_id":"ws-1","workflow_name":"Comment to DM","workflow_revision":3,"mode":"live","state":"cancelled","revision":2,"definition":{},"steps":[{"step_id":"reply","state":"cancelled"}],"error":"` + reason + `","created_at":"2026-11-10T09:00:00Z","updated_at":"2026-11-10T09:00:01Z"}]`},
		{method: "GET", path: "/api/v1/workflow-run-summaries", reply: `[]`},
		{method: "GET", path: "/api/v1/workflow-runs/run-1", reply: `{"id":"run-1","workflow_id":"wf-1","workspace_id":"ws-1","workflow_name":"Comment to DM","workflow_revision":3,"mode":"live","state":"failed","revision":2,"definition":{},"source":{"engagement_id":"e-1"},"steps":[{"step_id":"reply","kind":"private_comment_reply","name":"Send DM","state":"failed","inputs":{},"output":{"reason":"A connected account wrote this comment, so no private reply was sent."},"started_at":"2026-11-10T09:00:00Z"},{"step_id":"after","kind":"wait","name":"Wait","state":"failed","inputs":{},"output":{},"error":"provider refused the message","started_at":"2026-11-10T09:00:00Z"}],"error":"step failed","created_at":"2026-11-10T09:00:00Z","updated_at":"2026-11-10T09:00:01Z"}`},
	})
	runs, err := runWorkflowCommand(t, server, "workflow", "runs", "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"run-1", "cancelled", "live", reason} {
		if !strings.Contains(runs, want) {
			t.Errorf("runs output lacks %q:\n%s", want, runs)
		}
	}
	if _, err := runWorkflowCommand(t, server, "workflow", "runs"); err != nil {
		t.Fatal(err)
	}
	run, err := runWorkflowCommand(t, server, "workflow", "run", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"step failed", "A connected account wrote this comment", "provider refused the message", "private_comment_reply"} {
		if !strings.Contains(run, want) {
			t.Errorf("run output lacks %q:\n%s", want, run)
		}
	}
}

func TestWorkflowRunApproveBindsReviewedPostRevisionAndCancelUsesRunRevision(t *testing.T) {
	waiting := `{"id":"run-1","workflow_id":"wf-1","workspace_id":"ws-1","workflow_name":"Launch","workflow_revision":1,"mode":"live","state":"awaiting_approval","revision":6,"definition":{},"source":{},"current_step_id":"review","steps":[{"step_id":"review","kind":"approval","name":"Review","state":"awaiting_approval","inputs":{"publication_id":"post-9"},"output":{},"started_at":"2026-11-10T09:00:00Z"}],"created_at":"2026-11-10T09:00:00Z","updated_at":"2026-11-10T09:00:01Z"}`
	done := strings.Replace(waiting, `"state":"awaiting_approval","revision":6`, `"state":"running","revision":7`, 1)
	server := workflowServer(t, []workflowCall{
		{method: "GET", path: "/api/v1/workflow-runs/run-1", reply: waiting},
		{method: "GET", path: "/api/v1/workflow-runs/run-1", reply: waiting},
		{method: "POST", path: "/api/v1/workflow-runs/run-1/approve", body: map[string]any{"expected_revision": float64(6), "publication_revision": float64(12)}, reply: done},
		{method: "GET", path: "/api/v1/workflow-runs/run-1", reply: waiting},
		{method: "POST", path: "/api/v1/workflow-runs/run-1/cancel", body: map[string]any{"expected_revision": float64(6)}, reply: done},
	})
	_, err := runWorkflowCommand(t, server, "--yes", "workflow", "run", "approve", "run-1")
	if err == nil || !strings.Contains(err.Error(), "post-9") || !strings.Contains(err.Error(), "--post-revision") {
		t.Fatalf("approving a post review without its reviewed revision must fail, got %v", err)
	}
	if _, err := runWorkflowCommand(t, server, "--yes", "workflow", "run", "approve", "run-1", "--post-revision", "12"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWorkflowCommand(t, server, "--yes", "workflow", "run", "cancel", "run-1"); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowSampleSourceAndTestNodeSendDryRequests(t *testing.T) {
	dataFile := t.TempDir() + "/data.json"
	writeFile(t, dataFile, `{"title":"Hello"}`)
	server := workflowServer(t, []workflowCall{
		{method: "GET", path: "/api/v1/workflows/wf-1", reply: workflowFixtureJSON},
		{method: "POST", path: "/api/v1/workflow-sources/sample", body: map[string]any{"kind": "comment_received"}, reply: `[{"id":"c-1","title":"","body":"LINK please","url":"https://example.test/c-1"}]`},
		{method: "GET", path: "/api/v1/workflows/wf-1", reply: workflowFixtureJSON},
		{method: "POST", path: "/api/v1/workflows/wf-1/test-node", body: map[string]any{"expected_revision": float64(4), "step_id": "reply", "data": map[string]any{"title": "Hello"}}, reply: `{"id":"run-t","workflow_id":"wf-1","workspace_id":"ws-1","workflow_name":"Comment to DM","workflow_revision":4,"mode":"preview","state":"succeeded","revision":1,"definition":{},"source":{},"steps":[],"created_at":"2026-11-10T09:00:00Z","updated_at":"2026-11-10T09:00:00Z"}`},
	})
	sampled, err := runWorkflowCommand(t, server, "workflow", "sample-source", "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sampled, "LINK please") {
		t.Errorf("sample output = %s", sampled)
	}
	tested, err := runWorkflowCommand(t, server, "workflow", "test-node", "wf-1", "--step", "reply", "--data", dataFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tested, "succeeded") {
		t.Errorf("test-node output = %s", tested)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
