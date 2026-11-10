package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/openpost/backend/internal/services/workflows"
)

const (
	mcpToolWorkflowList       = "list_workflows"
	mcpToolWorkflowGet        = "get_workflow"
	mcpToolWorkflowRuns       = "list_workflow_runs"
	mcpToolWorkflowRun        = "get_workflow_run"
	mcpToolWorkflowCreate     = "create_workflow"
	mcpToolWorkflowSave       = "save_workflow"
	mcpToolWorkflowPublish    = "publish_workflow"
	mcpToolWorkflowPause      = "pause_workflow"
	mcpToolWorkflowStop       = "stop_workflow_replies"
	mcpToolWorkflowDelete     = "delete_workflow"
	mcpToolWorkflowRunApprove = "approve_workflow_run"
	mcpToolWorkflowRunCancel  = "cancel_workflow_run"
)

// SetWorkflowService wires the workflow service that backs the workflow tools.
// The tools call the same service methods as the REST handlers, so permission
// levels, revision checks and workspace boundaries stay in one place.
func (h *MCPHandler) SetWorkflowService(service *workflows.Service) { h.workflows = service }

var mcpWorkflowToolStatuses = map[string]mcpToolStatus{
	mcpToolWorkflowList:       {Invoking: "Loading workflows", Invoked: "Workflows loaded"},
	mcpToolWorkflowGet:        {Invoking: "Loading workflow", Invoked: "Workflow loaded"},
	mcpToolWorkflowRuns:       {Invoking: "Loading workflow runs", Invoked: "Workflow runs loaded"},
	mcpToolWorkflowRun:        {Invoking: "Loading workflow run", Invoked: "Workflow run loaded"},
	mcpToolWorkflowCreate:     {Invoking: "Creating workflow", Invoked: "Workflow created"},
	mcpToolWorkflowSave:       {Invoking: "Saving workflow", Invoked: "Workflow saved"},
	mcpToolWorkflowPublish:    {Invoking: "Publishing workflow", Invoked: "Workflow published"},
	mcpToolWorkflowPause:      {Invoking: "Pausing workflow", Invoked: "Workflow paused"},
	mcpToolWorkflowStop:       {Invoking: "Stopping workflow replies", Invoked: "Workflow replies stopped"},
	mcpToolWorkflowDelete:     {Invoking: "Deleting workflow", Invoked: "Workflow deleted"},
	mcpToolWorkflowRunApprove: {Invoking: "Approving workflow run", Invoked: "Workflow run approved"},
	mcpToolWorkflowRunCancel:  {Invoking: "Cancelling workflow run", Invoked: "Workflow run cancelled"},
}

func mcpWorkflowTools() []mcpOperationDefinition {
	text := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	revision := func(description string) map[string]any {
		return map[string]any{"type": "integer", "minimum": 1, "description": description}
	}
	workspace := text("Workspace ID returned by list_workspaces.")
	workflowID := text("Workflow ID returned by list_workflows.")
	runID := text("Run ID returned by list_workflow_runs.")
	definition := mcpOpenObjectSchema()
	definition["description"] = "Workflow definition: {schema: 1, source: {kind, ...}, steps: [{id, kind, name, inputs}]}. The server validates it exactly as the web editor does; read an existing workflow with get_workflow for a working example."
	definition["examples"] = []any{map[string]any{"schema": 1, "source": map[string]any{"kind": "manual"}, "steps": []any{
		map[string]any{"id": "draft", "kind": "create_draft", "name": "Draft", "inputs": map[string]any{"text": map[string]any{"reference": "source.title"}}},
	}}}
	workflowRevision := revision("Workflow revision returned by get_workflow. A changed revision fails with a conflict and nothing is overwritten; reload the workflow before retrying.")
	runRevision := revision("Run revision returned by get_workflow_run. A changed run fails with a conflict; reload the run before retrying.")
	tool := func(name, title, description string, properties map[string]any, required []string, mode mcpOperationMode, openWorld bool) mcpOperationDefinition {
		return mcpOperationDescriptor(map[string]any{
			"name": name, "title": title, "description": description,
			"inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false},
		}, mode, false, openWorld)
	}
	revisionTool := func(name, title, description string, openWorld bool) mcpOperationDefinition {
		return tool(name, title, description, map[string]any{"workspace_id": workspace, "workflow_id": workflowID, "expected_revision": workflowRevision}, []string{"workspace_id", "workflow_id", "expected_revision"}, mcpOperationExecute, openWorld)
	}
	return []mcpOperationDefinition{
		tool(mcpToolWorkflowList, "List workflows", "List the workspace's workflows with revision, published revision, enabled state and last source check. Read-only.", map[string]any{"workspace_id": workspace}, []string{"workspace_id"}, mcpOperationQuery, false),
		tool(mcpToolWorkflowGet, "Read workflow", "Read one workflow: its definition, revision, published revision, enabled state, source error and last check. Use its revision as expected_revision for changes. Read-only.", map[string]any{"workspace_id": workspace, "workflow_id": workflowID}, []string{"workspace_id", "workflow_id"}, mcpOperationQuery, false),
		tool(mcpToolWorkflowRuns, "List workflow runs", "List the latest 50 runs with state and stop reason (the error field), without source data or step payloads. Filter by workflow_id. Open one run with get_workflow_run. Read-only.", map[string]any{"workspace_id": workspace, "workflow_id": workflowID}, []string{"workspace_id"}, mcpOperationQuery, false),
		tool(mcpToolWorkflowRun, "Read workflow run", "Read one run with each step's inputs, output and error, including stop reasons such as a sender's daily limit, a conversation takeover or a connected-account commenter. Read-only.", map[string]any{"workspace_id": workspace, "run_id": runID}, []string{"workspace_id", "run_id"}, mcpOperationQuery, false),
		tool(mcpToolWorkflowCreate, "Create workflow", "Save a new workflow draft from a definition. It starts disabled and runs nothing until publish_workflow. Returns the workflow with its first revision.", map[string]any{"workspace_id": workspace, "name": map[string]any{"type": "string", "minLength": 1, "maxLength": 100, "description": "Workflow name."}, "description": map[string]any{"type": "string", "maxLength": 500, "description": "Optional description."}, "definition": definition}, []string{"workspace_id", "name", "definition"}, mcpOperationExecute, false),
		tool(mcpToolWorkflowSave, "Save workflow", "Replace a workflow draft. Fails with a revision conflict, without overwriting, when expected_revision is no longer current. A published workflow keeps running its published revision until publish_workflow. Returns the workflow with its new revision.", map[string]any{"workspace_id": workspace, "workflow_id": workflowID, "name": map[string]any{"type": "string", "minLength": 1, "maxLength": 100, "description": "Workflow name."}, "description": map[string]any{"type": "string", "maxLength": 500, "description": "Optional description."}, "expected_revision": workflowRevision, "definition": definition}, []string{"workspace_id", "workflow_id", "name", "expected_revision", "definition"}, mcpOperationExecute, false),
		revisionTool(mcpToolWorkflowPublish, "Publish workflow", "Publish the draft revision and enable its source, so new matching activity starts runs and can send automatic replies. Requires workspace administration. Fails with a revision conflict when expected_revision is not current. Returns the workflow.", true),
		revisionTool(mcpToolWorkflowPause, "Pause workflow", "Stop new automatic runs. Existing runs continue and pending replies are not cancelled; use stop_workflow_replies for those. Returns the workflow.", false),
		revisionTool(mcpToolWorkflowStop, "Stop workflow replies", "Stop new runs and cancel pending automatic comment replies. Returns the workflow.", false),
		tool(mcpToolWorkflowDelete, "Delete workflow", "Permanently delete a paused workflow with no active runs, together with its run history. Created posts remain. Requires workspace administration.", map[string]any{"workspace_id": workspace, "workflow_id": workflowID}, []string{"workspace_id", "workflow_id"}, mcpOperationExecute, false),
		tool(mcpToolWorkflowRunApprove, "Approve workflow run", "Approve a run waiting for review and continue it. For a post approval, publication_revision must be the reviewed post revision; a changed post fails and nothing is approved. For a reviewed reply, reply_text optionally replaces the proposed text. Returns the run.", map[string]any{"workspace_id": workspace, "run_id": runID, "expected_revision": runRevision, "publication_revision": map[string]any{"type": "integer", "minimum": 0, "description": "Post revision that was reviewed. Required for post approvals."}, "reply_text": map[string]any{"type": "string", "maxLength": 20000, "description": "Optional replacement reply text for a reviewed comment or message reply."}}, []string{"workspace_id", "run_id", "expected_revision"}, mcpOperationExecute, true),
		tool(mcpToolWorkflowRunCancel, "Cancel workflow run", "Cancel the remaining steps of a run. An action already dispatched may finish, and created drafts or queued posts remain. Returns the run.", map[string]any{"workspace_id": workspace, "run_id": runID, "expected_revision": runRevision}, []string{"workspace_id", "run_id", "expected_revision"}, mcpOperationExecute, false),
	}
}

func mcpWorkflowOutputSchema(toolName string) (map[string]any, bool) {
	switch toolName {
	case mcpToolWorkflowList:
		return mcpStructuredOutputSchema(map[string]any{"workflows": mcpArraySchema(mcpOpenObjectSchema())}, "workflows"), true
	case mcpToolWorkflowRuns:
		return mcpStructuredOutputSchema(map[string]any{"runs": mcpArraySchema(mcpOpenObjectSchema())}, "runs"), true
	case mcpToolWorkflowGet, mcpToolWorkflowCreate, mcpToolWorkflowSave, mcpToolWorkflowPublish, mcpToolWorkflowPause, mcpToolWorkflowStop:
		return mcpStructuredOutputSchema(map[string]any{"workflow": mcpOpenObjectSchema()}, "workflow"), true
	case mcpToolWorkflowRun, mcpToolWorkflowRunApprove, mcpToolWorkflowRunCancel:
		return mcpStructuredOutputSchema(map[string]any{"run": mcpOpenObjectSchema()}, "run"), true
	case mcpToolWorkflowDelete:
		return mcpStructuredOutputSchema(map[string]any{"deleted": map[string]any{"type": "boolean"}, "workflow_id": map[string]any{"type": "string"}}, "deleted", "workflow_id"), true
	}
	return nil, false
}

type mcpWorkflowInput struct {
	WorkspaceID         string          `json:"workspace_id"`
	WorkflowID          string          `json:"workflow_id"`
	RunID               string          `json:"run_id"`
	Name                string          `json:"name"`
	Description         string          `json:"description"`
	Definition          json.RawMessage `json:"definition"`
	ExpectedRevision    int             `json:"expected_revision"`
	PublicationRevision int             `json:"publication_revision"`
	ReplyText           *string         `json:"reply_text"`
}

func (h *MCPHandler) callWorkflowTool(ctx context.Context, userID, name string, args map[string]any) (any, *mcpError) {
	var input mcpWorkflowInput
	if err := decodeMCPArguments(args, &input); err != nil {
		return nil, &mcpError{Code: -32602, Message: "invalid " + name + " arguments"}
	}
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	if rpcErr := h.ensureWorkspaceAccess(ctx, userID, input.WorkspaceID); rpcErr != nil {
		return nil, rpcErr
	}
	if h.workflows == nil {
		return nil, &mcpError{Code: -32603, Message: "Workflows are unavailable"}
	}
	structured, summary, err := h.executeWorkflowTool(ctx, userID, name, input)
	if err != nil {
		return nil, mcpWorkflowError(err)
	}
	return map[string]any{
		"content":           []mcpContent{{Type: "text", Text: summary}},
		"structuredContent": structured,
	}, nil
}

//nolint:gocyclo // Each case delegates one tool to its owning service method.
func (h *MCPHandler) executeWorkflowTool(ctx context.Context, userID, name string, input mcpWorkflowInput) (map[string]any, string, error) {
	actor := workspaceActor(ctx, userID)
	workspaceID := input.WorkspaceID
	workflowResult := func(item workflows.Workflow, err error, verb string) (map[string]any, string, error) {
		if err != nil {
			return nil, "", err
		}
		return map[string]any{"workflow": item}, fmt.Sprintf("Workflow %s %s at revision %d.", item.ID, verb, item.Revision), nil
	}
	runResult := func(item workflows.Run, err error, verb string) (map[string]any, string, error) {
		if err != nil {
			return nil, "", err
		}
		return map[string]any{"run": item}, fmt.Sprintf("Workflow run %s %s; state %s.", item.ID, verb, item.State), nil
	}
	switch name {
	case mcpToolWorkflowList:
		items, err := h.workflows.List(ctx, actor, workspaceID)
		if err != nil {
			return nil, "", err
		}
		return map[string]any{"workflows": items}, fmt.Sprintf("%d workflows.", len(items)), nil
	case mcpToolWorkflowGet:
		item, err := h.workflows.Get(ctx, actor, workspaceID, input.WorkflowID)
		return workflowResult(item, err, "loaded")
	case mcpToolWorkflowRuns:
		items, err := h.workflows.RunSummaries(ctx, actor, workspaceID, input.WorkflowID)
		if err != nil {
			return nil, "", err
		}
		return map[string]any{"runs": items}, fmt.Sprintf("%d workflow runs.", len(items)), nil
	case mcpToolWorkflowRun:
		item, err := h.workflows.GetRun(ctx, actor, workspaceID, input.RunID)
		return runResult(item, err, "loaded")
	case mcpToolWorkflowCreate, mcpToolWorkflowSave:
		request, err := input.saveRequest()
		if err != nil {
			return nil, "", err
		}
		if name == mcpToolWorkflowCreate {
			item, err := h.workflows.Save(ctx, actor, workspaceID, "", request)
			return workflowResult(item, err, "created")
		}
		item, err := h.workflows.Save(ctx, actor, workspaceID, input.WorkflowID, request)
		return workflowResult(item, err, "saved")
	case mcpToolWorkflowPublish:
		item, err := h.workflows.Publish(ctx, actor, workspaceID, input.WorkflowID, input.ExpectedRevision)
		return workflowResult(item, err, "published")
	case mcpToolWorkflowPause:
		item, err := h.workflows.Pause(ctx, actor, workspaceID, input.WorkflowID, input.ExpectedRevision)
		return workflowResult(item, err, "paused")
	case mcpToolWorkflowStop:
		item, err := h.workflows.StopReplies(ctx, actor, workspaceID, input.WorkflowID, input.ExpectedRevision)
		return workflowResult(item, err, "stopped replies")
	case mcpToolWorkflowDelete:
		if err := h.workflows.Delete(ctx, actor, workspaceID, input.WorkflowID); err != nil {
			return nil, "", err
		}
		return map[string]any{"deleted": true, "workflow_id": input.WorkflowID}, "Workflow " + input.WorkflowID + " deleted.", nil
	case mcpToolWorkflowRunApprove:
		var replyText []string
		if input.ReplyText != nil {
			replyText = []string{*input.ReplyText}
		}
		item, err := h.workflows.Approve(ctx, actor, workspaceID, input.RunID, input.ExpectedRevision, input.PublicationRevision, replyText...)
		return runResult(item, err, "approved")
	default:
		item, err := h.workflows.Cancel(ctx, actor, workspaceID, input.RunID, input.ExpectedRevision)
		return runResult(item, err, "cancelled")
	}
}

func (input mcpWorkflowInput) saveRequest() (workflows.SaveRequest, error) {
	request := workflows.SaveRequest{Name: input.Name, Description: input.Description, ExpectedRevision: input.ExpectedRevision}
	decoder := json.NewDecoder(bytes.NewReader(input.Definition))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request.Definition); err != nil {
		return request, fmt.Errorf("%w: definition: %s", workflows.ErrInvalid, err)
	}
	return request, nil
}

// mcpWorkflowError keeps the service's wording for caller mistakes, including
// revision conflicts, and hides internal failures behind a generic message.
func mcpWorkflowError(err error) *mcpError {
	var status huma.StatusError
	if errors.As(workflowHTTPError(err), &status) && status.GetStatus() < 500 {
		return &mcpError{Code: -32602, Message: status.Error()}
	}
	return &mcpError{Code: -32603, Message: "The workflow operation could not be completed. Try again."}
}
