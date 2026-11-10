package api

import (
	"context"
	"encoding/json"
	"net/url"
)

// Workflow types mirror the Workflow, WorkflowRun, WorkflowRunSummary and
// WorkflowSourceItem schemas in apps/web/openapi.json. A definition is kept as
// raw JSON in both directions: the CLI never reshapes what the server stores or
// validates, so --json output and saved files stay faithful to the API.

// Workflow is a saved automation with its draft revision and published state.
type Workflow struct {
	ID                string          `json:"id"`
	WorkspaceID       string          `json:"workspace_id"`
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	Revision          int             `json:"revision"`
	PublishedRevision int             `json:"published_revision"`
	Enabled           bool            `json:"enabled"`
	HasCommentReplies bool            `json:"has_comment_replies,omitempty"`
	Definition        json.RawMessage `json:"definition"`
	SourceError       string          `json:"source_error,omitempty"`
	LastCheckedAt     string          `json:"last_checked_at,omitempty"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
}

// WorkflowStepResult is one executed step of a run.
type WorkflowStepResult struct {
	StepID      string         `json:"step_id"`
	Kind        string         `json:"kind"`
	Name        string         `json:"name"`
	State       string         `json:"state"`
	Inputs      map[string]any `json:"inputs"`
	Output      map[string]any `json:"output"`
	Error       string         `json:"error,omitempty"`
	StartedAt   string         `json:"started_at"`
	CompletedAt string         `json:"completed_at,omitempty"`
}

// WorkflowRun is the full record of one run, including source data and step
// inputs and outputs.
type WorkflowRun struct {
	ID               string               `json:"id"`
	WorkflowID       string               `json:"workflow_id"`
	WorkspaceID      string               `json:"workspace_id"`
	WorkflowName     string               `json:"workflow_name"`
	WorkflowRevision int                  `json:"workflow_revision"`
	Mode             string               `json:"mode"`
	State            string               `json:"state"`
	Revision         int                  `json:"revision"`
	Definition       json.RawMessage      `json:"definition"`
	Source           map[string]any       `json:"source"`
	Steps            []WorkflowStepResult `json:"steps"`
	CurrentStepID    string               `json:"current_step_id,omitempty"`
	Error            string               `json:"error,omitempty"`
	WakeAt           string               `json:"wake_at,omitempty"`
	CreatedAt        string               `json:"created_at"`
	UpdatedAt        string               `json:"updated_at"`
}

// WorkflowStepSummary is the state of one step in a run summary.
type WorkflowStepSummary struct {
	StepID string `json:"step_id"`
	State  string `json:"state"`
}

// WorkflowRunSummary is a run without source data or step payloads. Error
// carries the stop reason when the server ended or refused the run.
type WorkflowRunSummary struct {
	ID               string                `json:"id"`
	WorkflowID       string                `json:"workflow_id"`
	WorkspaceID      string                `json:"workspace_id"`
	WorkflowName     string                `json:"workflow_name"`
	WorkflowRevision int                   `json:"workflow_revision"`
	Mode             string                `json:"mode"`
	State            string                `json:"state"`
	Revision         int                   `json:"revision"`
	Definition       json.RawMessage       `json:"definition"`
	Steps            []WorkflowStepSummary `json:"steps"`
	CurrentStepID    string                `json:"current_step_id,omitempty"`
	Error            string                `json:"error,omitempty"`
	WakeAt           string                `json:"wake_at,omitempty"`
	CreatedAt        string                `json:"created_at"`
	UpdatedAt        string                `json:"updated_at"`
}

// WorkflowSourceItem is one example read from a workflow source.
type WorkflowSourceItem struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	URL            string `json:"url"`
	AccountID      string `json:"account_id,omitempty"`
	AuthorName     string `json:"author_name,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
	EngagementID   string `json:"engagement_id,omitempty"`
	MessageID      string `json:"message_id,omitempty"`
	Platform       string `json:"platform,omitempty"`
	PublicationID  string `json:"publication_id,omitempty"`
	PublishedAt    string `json:"published_at,omitempty"`
	RenditionID    string `json:"rendition_id,omitempty"`
}

// SaveWorkflowInput is the WorkflowSaveRequest body. ExpectedRevision is 0 for
// a new workflow and the revision being edited otherwise.
type SaveWorkflowInput struct {
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	ExpectedRevision int             `json:"expected_revision"`
	Definition       json.RawMessage `json:"definition"`
}

// ApproveWorkflowRunInput binds an approval to the reviewed run revision and,
// for post approvals, the reviewed post revision.
type ApproveWorkflowRunInput struct {
	ExpectedRevision    int     `json:"expected_revision"`
	PublicationRevision int     `json:"publication_revision,omitempty"`
	ReplyText           *string `json:"reply_text,omitempty"`
}

// TestWorkflowNodeInput runs one data or tool node with supplied input data.
type TestWorkflowNodeInput struct {
	ExpectedRevision int            `json:"expected_revision"`
	StepID           string         `json:"step_id"`
	Data             map[string]any `json:"data"`
}

// WorkflowDeleteResult is the body of a successful workflow delete.
type WorkflowDeleteResult struct {
	Deleted bool `json:"deleted"`
}

type workflowRevisionBody struct {
	ExpectedRevision int `json:"expected_revision"`
}

func workflowPath(workspaceID, path string, extra url.Values) string {
	query := url.Values{}
	query.Set("workspace_id", workspaceID)
	for key, values := range extra {
		query[key] = values
	}
	return "/api/v1" + path + "?" + query.Encode()
}

func (c *Client) ListWorkflows(ctx context.Context, workspaceID string) ([]Workflow, error) {
	var out []Workflow
	if err := c.GetJSON(ctx, workflowPath(workspaceID, "/workflows", nil), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetWorkflow(ctx context.Context, workspaceID, id string) (*Workflow, error) {
	var out Workflow
	if err := c.GetJSON(ctx, workflowPath(workspaceID, "/workflows/"+url.PathEscape(id), nil), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateWorkflow(ctx context.Context, workspaceID string, in SaveWorkflowInput) (*Workflow, error) {
	var out Workflow
	if err := c.PostJSON(ctx, workflowPath(workspaceID, "/workflows", nil), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SaveWorkflow(ctx context.Context, workspaceID, id string, in SaveWorkflowInput) (*Workflow, error) {
	var out Workflow
	if err := c.PutJSON(ctx, workflowPath(workspaceID, "/workflows/"+url.PathEscape(id), nil), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) workflowAction(ctx context.Context, workspaceID, id, action string, expectedRevision int) (*Workflow, error) {
	var out Workflow
	path := workflowPath(workspaceID, "/workflows/"+url.PathEscape(id)+"/"+action, nil)
	if err := c.PostJSON(ctx, path, workflowRevisionBody{ExpectedRevision: expectedRevision}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) PublishWorkflow(ctx context.Context, workspaceID, id string, expectedRevision int) (*Workflow, error) {
	return c.workflowAction(ctx, workspaceID, id, "publish", expectedRevision)
}

func (c *Client) PauseWorkflow(ctx context.Context, workspaceID, id string, expectedRevision int) (*Workflow, error) {
	return c.workflowAction(ctx, workspaceID, id, "pause", expectedRevision)
}

func (c *Client) StopWorkflowReplies(ctx context.Context, workspaceID, id string, expectedRevision int) (*Workflow, error) {
	return c.workflowAction(ctx, workspaceID, id, "stop-replies", expectedRevision)
}

func (c *Client) DeleteWorkflow(ctx context.Context, workspaceID, id string) (*WorkflowDeleteResult, error) {
	var out WorkflowDeleteResult
	if err := c.DeleteJSON(ctx, workflowPath(workspaceID, "/workflows/"+url.PathEscape(id), nil), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListWorkflowRunSummaries returns the latest 50 runs. An empty workflowID
// lists runs of every workflow in the workspace.
func (c *Client) ListWorkflowRunSummaries(ctx context.Context, workspaceID, workflowID string) ([]WorkflowRunSummary, error) {
	extra := url.Values{}
	if workflowID != "" {
		extra.Set("workflow_id", workflowID)
	}
	var out []WorkflowRunSummary
	if err := c.GetJSON(ctx, workflowPath(workspaceID, "/workflow-run-summaries", extra), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetWorkflowRun(ctx context.Context, workspaceID, id string) (*WorkflowRun, error) {
	var out WorkflowRun
	if err := c.GetJSON(ctx, workflowPath(workspaceID, "/workflow-runs/"+url.PathEscape(id), nil), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ApproveWorkflowRun(ctx context.Context, workspaceID, id string, in ApproveWorkflowRunInput) (*WorkflowRun, error) {
	var out WorkflowRun
	if err := c.PostJSON(ctx, workflowPath(workspaceID, "/workflow-runs/"+url.PathEscape(id)+"/approve", nil), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CancelWorkflowRun(ctx context.Context, workspaceID, id string, expectedRevision int) (*WorkflowRun, error) {
	var out WorkflowRun
	path := workflowPath(workspaceID, "/workflow-runs/"+url.PathEscape(id)+"/cancel", nil)
	if err := c.PostJSON(ctx, path, workflowRevisionBody{ExpectedRevision: expectedRevision}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SampleWorkflowSource reads examples from a source definition without
// advancing a production cursor.
func (c *Client) SampleWorkflowSource(ctx context.Context, workspaceID string, source json.RawMessage) ([]WorkflowSourceItem, error) {
	var out []WorkflowSourceItem
	if err := c.PostJSON(ctx, workflowPath(workspaceID, "/workflow-sources/sample", nil), source, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) TestWorkflowNode(ctx context.Context, workspaceID, id string, in TestWorkflowNodeInput) (*WorkflowRun, error) {
	var out WorkflowRun
	if err := c.PostJSON(ctx, workflowPath(workspaceID, "/workflows/"+url.PathEscape(id)+"/test-node", nil), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
