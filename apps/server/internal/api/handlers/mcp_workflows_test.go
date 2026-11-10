package handlers

import (
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/openpost/backend/internal/services/workflows"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/stretchr/testify/require"
)

func newMCPWorkflowServer(t *testing.T) *mcpTestServer {
	t.Helper()
	db := workflowHandlerDB(t)
	service := workflows.NewService(db, nil, nil)
	handler := NewMCPHandler(db, mcpScopeAuthenticator{
		"read":  {UserID: "user", Scope: "mcp:read", WorkspaceID: "ws"},
		"write": {UserID: "user", Scope: "mcp:full", WorkspaceID: "ws"},
		"other": {UserID: "user", Scope: "mcp:full", WorkspaceID: "elsewhere"},
	})
	handler.SetWorkflowService(service)
	e := echo.New()
	handler.RegisterRoutes(e)
	return &mcpTestServer{echo: e, db: db, handler: handler}
}

var mcpWorkflowDefinition = map[string]any{"schema": 1, "source": map[string]any{"kind": "manual"}, "steps": []any{
	map[string]any{"id": "draft", "kind": "create_draft", "name": "Draft", "inputs": map[string]any{"text": map[string]any{"reference": "source.title"}}},
}}

func TestMCPWorkflowToolsAreLabelledByEffect(t *testing.T) {
	t.Parallel()
	reads := map[string]bool{mcpToolWorkflowList: true, mcpToolWorkflowGet: true, mcpToolWorkflowRuns: true, mcpToolWorkflowRun: true}
	writes := []string{mcpToolWorkflowCreate, mcpToolWorkflowSave, mcpToolWorkflowPublish, mcpToolWorkflowPause, mcpToolWorkflowStop, mcpToolWorkflowDelete, mcpToolWorkflowRunApprove, mcpToolWorkflowRunCancel}
	require.Equal(t, []string{"list_workflows", "get_workflow", "list_workflow_runs", "get_workflow_run", "create_workflow", "save_workflow", "publish_workflow", "pause_workflow", "stop_workflow_replies", "delete_workflow", "approve_workflow_run", "cancel_workflow_run"},
		[]string{mcpToolWorkflowList, mcpToolWorkflowGet, mcpToolWorkflowRuns, mcpToolWorkflowRun, mcpToolWorkflowCreate, mcpToolWorkflowSave, mcpToolWorkflowPublish, mcpToolWorkflowPause, mcpToolWorkflowStop, mcpToolWorkflowDelete, mcpToolWorkflowRunApprove, mcpToolWorkflowRunCancel})
	seen := map[string]bool{}
	for _, operation := range mcpWorkflowTools() {
		name := operation.Descriptor["name"].(string)
		seen[name] = true
		annotations := operation.Descriptor["annotations"].(map[string]any)
		if reads[name] {
			require.Equal(t, mcpOperationQuery, operation.Mode, name)
			require.Equal(t, true, annotations["readOnlyHint"], name)
			require.Equal(t, false, annotations["destructiveHint"], name)
			continue
		}
		require.Equal(t, mcpOperationExecute, operation.Mode, name)
		require.Equal(t, false, annotations["readOnlyHint"], name)
		require.Equal(t, true, annotations["destructiveHint"], name)
	}
	for _, name := range writes {
		require.True(t, seen[name], name)
	}
	for name := range reads {
		require.True(t, seen[name], name)
	}
	for _, operation := range mcpOperationCatalog() {
		name := operation.Descriptor["name"].(string)
		if reads[name] || seen[name] {
			delete(seen, name)
		}
	}
	require.Empty(t, seen, "every workflow tool is in the operation catalog")
}

func TestMCPWorkflowLifecycleRevisionConflictAndReadScope(t *testing.T) {
	srv := newMCPWorkflowServer(t)
	call := func(token, tool string, args map[string]any) map[string]any {
		return mcpCallOperation(t, srv, token, tool, tool, args)
	}
	created := mcpStructuredContent(t, call("write", mcpToolWorkflowCreate, map[string]any{"workspace_id": "ws", "name": "Launch", "definition": mcpWorkflowDefinition}))
	workflow := created["workflow"].(map[string]any)
	id := workflow["id"].(string)
	require.EqualValues(t, 1, workflow["revision"])
	require.Equal(t, false, workflow["enabled"])

	saved := mcpStructuredContent(t, call("write", mcpToolWorkflowSave, map[string]any{"workspace_id": "ws", "workflow_id": id, "name": "Launch v2", "expected_revision": 1, "definition": mcpWorkflowDefinition}))
	require.EqualValues(t, 2, saved["workflow"].(map[string]any)["revision"])

	stale := call("write", mcpToolWorkflowSave, map[string]any{"workspace_id": "ws", "workflow_id": id, "name": "Overwrite", "expected_revision": 1, "definition": mcpWorkflowDefinition})
	require.Contains(t, mcpErrorMessage(t, stale), "workflow changed")
	read := mcpStructuredContent(t, call("read", mcpToolWorkflowGet, map[string]any{"workspace_id": "ws", "workflow_id": id}))
	require.Equal(t, "Launch v2", read["workflow"].(map[string]any)["name"], "a stale save must not overwrite")

	listed := mcpStructuredContent(t, call("read", mcpToolWorkflowList, map[string]any{"workspace_id": "ws"}))
	require.Len(t, listed["workflows"], 1)
	runs := mcpStructuredContent(t, call("read", mcpToolWorkflowRuns, map[string]any{"workspace_id": "ws", "workflow_id": id}))
	require.Empty(t, runs["runs"])

	require.Contains(t, mcpErrorMessage(t, call("read", mcpToolWorkflowDelete, map[string]any{"workspace_id": "ws", "workflow_id": id})), "mcp:read")
	require.Contains(t, mcpErrorMessage(t, call("read", mcpToolWorkflowPause, map[string]any{"workspace_id": "ws", "workflow_id": id, "expected_revision": 2})), "mcp:read")
	require.Contains(t, mcpErrorMessage(t, call("other", mcpToolWorkflowGet, map[string]any{"workspace_id": "ws", "workflow_id": id})), "token scope")

	paused := mcpStructuredContent(t, call("write", mcpToolWorkflowPause, map[string]any{"workspace_id": "ws", "workflow_id": id, "expected_revision": 2}))
	require.EqualValues(t, 3, paused["workflow"].(map[string]any)["revision"])
	deleted := mcpStructuredContent(t, call("write", mcpToolWorkflowDelete, map[string]any{"workspace_id": "ws", "workflow_id": id}))
	require.Equal(t, true, deleted["deleted"])
	missing := call("write", mcpToolWorkflowGet, map[string]any{"workspace_id": "ws", "workflow_id": id})
	require.Contains(t, mcpErrorMessage(t, missing), "workflow not found")
}

func TestMCPWorkflowToolsRejectInvalidDefinitionAndUnavailableService(t *testing.T) {
	srv := newMCPWorkflowServer(t)
	invalid := mcpCallOperation(t, srv, "write", "invalid", mcpToolWorkflowCreate, map[string]any{"workspace_id": "ws", "name": "Bad", "definition": map[string]any{"schema": 1, "source": map[string]any{"kind": "manual"}, "steps": []any{map[string]any{"id": "x", "kind": "not_a_step", "name": "X", "inputs": map[string]any{}}}}})
	require.NotNil(t, invalid["error"])
	srv.handler.workflows = nil
	unavailable := mcpCallOperation(t, srv, "write", "unavailable", mcpToolWorkflowList, map[string]any{"workspace_id": "ws"})
	require.Contains(t, mcpErrorMessage(t, unavailable), "unavailable")
}

func TestMCPWorkflowRunToolsReadStopReasonAndCancelByRevision(t *testing.T) {
	srv := newMCPWorkflowServer(t)
	call := func(token, tool string, args map[string]any) map[string]any {
		return mcpCallOperation(t, srv, token, tool, tool, args)
	}
	created := mcpStructuredContent(t, call("write", mcpToolWorkflowCreate, map[string]any{"workspace_id": "ws", "name": "Runs", "definition": mcpWorkflowDefinition}))
	id := created["workflow"].(map[string]any)["id"].(string)
	run, err := srv.handler.workflows.Start(t.Context(), workspaceaccess.ActorFacts{UserID: "user"}, "ws", id, workflows.ModePreview, map[string]any{"title": "Hello"}, 1)
	require.NoError(t, err)

	summaries := mcpStructuredContent(t, call("read", mcpToolWorkflowRuns, map[string]any{"workspace_id": "ws", "workflow_id": id}))
	require.Len(t, summaries["runs"], 1)
	summary := summaries["runs"].([]any)[0].(map[string]any)
	require.Equal(t, run.ID, summary["id"])
	require.NotContains(t, summary, "source", "summaries carry no source data")
	full := mcpStructuredContent(t, call("read", mcpToolWorkflowRun, map[string]any{"workspace_id": "ws", "run_id": run.ID}))
	require.Equal(t, run.ID, full["run"].(map[string]any)["id"])
	require.Contains(t, full["run"].(map[string]any), "steps")

	stale := call("write", mcpToolWorkflowRunCancel, map[string]any{"workspace_id": "ws", "run_id": run.ID, "expected_revision": run.Revision + 5})
	require.Contains(t, mcpErrorMessage(t, stale), "no longer in the expected state")
	cancelled := mcpStructuredContent(t, call("write", mcpToolWorkflowRunCancel, map[string]any{"workspace_id": "ws", "run_id": run.ID, "expected_revision": run.Revision}))
	require.Equal(t, workflows.StateCancelled, cancelled["run"].(map[string]any)["state"])
}
