package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/openpost/cli/internal/api"
	"github.com/openpost/cli/internal/config"
)

const workflowRevisionFlag = "expected-revision"

// newWorkflowCmd manages workflows (automations) through the same API the web
// app uses, so permissions, revision checks and workspace boundaries are the
// server's. A write never retries or overwrites after a revision conflict.
func newWorkflowCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "workflow", Short: "Create, publish, pause and inspect workflows"}
	cmd.AddCommand(
		newWorkflowListCmd(),
		newWorkflowGetCmd(),
		newWorkflowCreateCmd(),
		newWorkflowSaveCmd(),
		newWorkflowStateCmd("publish", "Publish the draft revision and enable its source", true, (*api.Client).PublishWorkflow,
			"Publish a workflow's draft revision and enable its source. New matching activity then starts runs, which can send replies."),
		newWorkflowStateCmd("pause", "Stop new automatic runs; existing runs continue", false, (*api.Client).PauseWorkflow,
			"Stop new automatic runs. Existing runs and pending replies continue; use stop-replies for pending replies."),
		newWorkflowStateCmd("stop-replies", "Stop new runs and cancel pending automatic replies", true, (*api.Client).StopWorkflowReplies,
			"Stop new runs and cancel pending automatic comment replies."),
		newWorkflowDeleteCmd(),
		newWorkflowRunsCmd(),
		newWorkflowRunCmd(),
		newWorkflowSampleSourceCmd(),
		newWorkflowTestNodeCmd(),
	)
	return cmd
}

func workflowRuntime(cmd *cobra.Command) (*config.Runtime, *api.Client, string, error) {
	cfg, err := runtimeFrom(cmd)
	if err != nil {
		return nil, nil, "", err
	}
	client, err := clientFrom(cfg)
	if err != nil {
		return nil, nil, "", err
	}
	workspaceID, err := activeWorkspaceID(cmd, client)
	if err != nil {
		return nil, nil, "", err
	}
	return cfg, client, workspaceID, nil
}

// confirmWorkflowAction asks before an action that publishes, sends, cancels or
// deletes. It returns false after printing "Canceled." when the answer is no.
func confirmWorkflowAction(cmd *cobra.Command, prompt string) (bool, error) {
	cfg, err := runtimeFrom(cmd)
	if err != nil {
		return false, err
	}
	ok, err := confirmMutation(cfg, prompt)
	if err != nil {
		return false, err
	}
	if !ok {
		printerFrom(cfg).Printf("Canceled.")
	}
	return ok, nil
}

// workflowStateLabel names the lifecycle state a person sees in the web app.
func workflowStateLabel(w api.Workflow) string {
	switch {
	case w.Enabled:
		return "active"
	case w.PublishedRevision > 0:
		return "paused"
	default:
		return "draft"
	}
}

type workflowStepInfo struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// workflowOutline reads the source kind and top-level steps of a raw
// definition. It returns empty values for a definition it cannot read.
func workflowOutline(definition json.RawMessage) (string, []workflowStepInfo) {
	var parsed struct {
		Source struct {
			Kind string `json:"kind"`
		} `json:"source"`
		Steps []workflowStepInfo `json:"steps"`
	}
	if json.Unmarshal(definition, &parsed) != nil {
		return "", nil
	}
	return parsed.Source.Kind, parsed.Steps
}

func printWorkflowSummary(cfg *config.Runtime, w *api.Workflow) error {
	p := printerFrom(cfg)
	if cfg.AsJSON {
		return p.PrintJSON(w)
	}
	source, _ := workflowOutline(w.Definition)
	p.Table([]string{"FIELD", "VALUE"}, [][]string{
		{"id", w.ID},
		{"name", w.Name},
		{"state", workflowStateLabel(*w)},
		{"revision", strconv.Itoa(w.Revision)},
		{"published_revision", strconv.Itoa(w.PublishedRevision)},
		{"source", emptyDash(source)},
		{"last_checked_at", emptyDash(w.LastCheckedAt)},
		{"source_error", emptyDash(w.SourceError)},
		{"updated_at", emptyDash(w.UpdatedAt)},
	})
	return nil
}

func newWorkflowListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List workflows",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, client, workspaceID, err := workflowRuntime(cmd)
			if err != nil {
				return err
			}
			items, err := client.ListWorkflows(cmd.Context(), workspaceID)
			if err != nil {
				return err
			}
			p := printerFrom(cfg)
			if cfg.AsJSON {
				return p.PrintJSON(items)
			}
			rows := make([][]string, 0, len(items))
			for _, w := range items {
				source, _ := workflowOutline(w.Definition)
				rows = append(rows, []string{w.ID, w.Name, workflowStateLabel(w), strconv.Itoa(w.Revision), strconv.Itoa(w.PublishedRevision), emptyDash(source), emptyDash(w.LastCheckedAt)})
			}
			p.Table([]string{"ID", "NAME", "STATE", "REVISION", "PUBLISHED", "SOURCE", "LAST CHECKED"}, rows)
			return nil
		},
	}
}

func newWorkflowGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <workflow-id>",
		Short: "Show a workflow, its revisions and definition",
		Long:  "Show a workflow's state, revision, published revision and last source check. With --json the output is the complete workflow; edit its definition and pass the file to `workflow save --file` to change it.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, workspaceID, err := workflowRuntime(cmd)
			if err != nil {
				return err
			}
			w, err := client.GetWorkflow(cmd.Context(), workspaceID, args[0])
			if err != nil {
				return err
			}
			if err := printWorkflowSummary(cfg, w); err != nil || cfg.AsJSON {
				return err
			}
			_, steps := workflowOutline(w.Definition)
			for _, step := range steps {
				printerFrom(cfg).Printf("step %s\t%s\t%s", step.ID, step.Kind, emptyDash(step.Name))
			}
			return nil
		},
	}
}

// workflowFile is a workflow definition file. It is either a bare definition
// (schema, source, steps) or an envelope that also carries the name,
// description and revision, such as the output of `workflow get --json`.
type workflowFile struct {
	Name             *string         `json:"name"`
	Description      *string         `json:"description"`
	Revision         int             `json:"revision"`
	ExpectedRevision int             `json:"expected_revision"`
	Definition       json.RawMessage `json:"definition"`
}

func readJSONInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func readWorkflowFile(path string) (workflowFile, error) {
	data, err := readJSONInput(path)
	if err != nil {
		return workflowFile{}, err
	}
	var file workflowFile
	if err := json.Unmarshal(data, &file); err != nil {
		return workflowFile{}, fmt.Errorf("%s is not a JSON object: %w", path, err)
	}
	if len(bytes.TrimSpace(file.Definition)) > 0 {
		return file, nil
	}
	var bare map[string]json.RawMessage
	if err := json.Unmarshal(data, &bare); err != nil {
		return workflowFile{}, fmt.Errorf("%s is not a JSON object: %w", path, err)
	}
	if _, ok := bare["steps"]; !ok {
		return workflowFile{}, fmt.Errorf("%s has no \"definition\" and no \"steps\": pass a workflow definition or the output of `workflow get --json`", path)
	}
	return workflowFile{Definition: json.RawMessage(data)}, nil
}

func addWorkflowFileFlags(cmd *cobra.Command) {
	cmd.Flags().String("file", "", "workflow definition JSON file, or - for stdin")
	cmd.Flags().String("name", "", "workflow name (default: the name in the file)")
	cmd.Flags().String("description", "", "workflow description (default: the description in the file)")
	_ = cmd.MarkFlagRequired("file")
}

// flagOrFile prefers an explicitly passed flag over the value in the file.
func flagOrFile(cmd *cobra.Command, flag string, fromFile *string) (string, bool) {
	if cmd.Flags().Changed(flag) {
		value, _ := cmd.Flags().GetString(flag)
		return value, true
	}
	if fromFile != nil {
		return *fromFile, true
	}
	return "", false
}

func newWorkflowCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create --file <definition.json>",
		Short: "Create a workflow draft from a JSON file",
		Long:  "Create a workflow draft. The file is a definition ({\"schema\":1,\"source\":{...},\"steps\":[...]}) or an object with name, description and definition. The draft is disabled and runs nothing until `workflow publish`.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("file")
			file, err := readWorkflowFile(path)
			if err != nil {
				return err
			}
			name, _ := flagOrFile(cmd, "name", file.Name)
			if strings.TrimSpace(name) == "" {
				return errors.New("a workflow name is required: pass --name or include \"name\" in the file")
			}
			description, _ := flagOrFile(cmd, "description", file.Description)
			cfg, client, workspaceID, err := workflowRuntime(cmd)
			if err != nil {
				return err
			}
			w, err := client.CreateWorkflow(cmd.Context(), workspaceID, api.SaveWorkflowInput{Name: name, Description: description, Definition: file.Definition})
			if err != nil {
				return err
			}
			return printWorkflowSummary(cfg, w)
		},
	}
	addWorkflowFileFlags(cmd)
	return cmd
}

func newWorkflowSaveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "save <workflow-id> --file <definition.json> --expected-revision <n>",
		Short: "Replace a workflow draft, checking its revision",
		Long: "Replace a workflow draft and print its new revision. The save fails with the server's revision conflict, and nothing is overwritten, when the workflow changed since the revision you pass.\n\n" +
			"The expected revision is --expected-revision, or the \"revision\" of a file saved from `workflow get --json`. It is never filled in from the server, because that would overwrite changes you have not seen. " +
			"A published workflow keeps running its published revision until `workflow publish`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := cmd.Flags().GetString("file")
			file, err := readWorkflowFile(path)
			if err != nil {
				return err
			}
			revision := file.ExpectedRevision
			if revision == 0 {
				revision = file.Revision
			}
			if cmd.Flags().Changed(workflowRevisionFlag) {
				revision, _ = cmd.Flags().GetInt(workflowRevisionFlag)
			}
			if revision < 1 {
				return errors.New("the revision this edit is based on is required: pass --expected-revision, or save from the output of `workflow get --json`")
			}
			cfg, client, workspaceID, err := workflowRuntime(cmd)
			if err != nil {
				return err
			}
			name, hasName := flagOrFile(cmd, "name", file.Name)
			description, hasDescription := flagOrFile(cmd, "description", file.Description)
			if !hasName || strings.TrimSpace(name) == "" || !hasDescription {
				current, err := client.GetWorkflow(cmd.Context(), workspaceID, args[0])
				if err != nil {
					return err
				}
				if !hasName || strings.TrimSpace(name) == "" {
					name = current.Name
				}
				if !hasDescription {
					description = current.Description
				}
			}
			w, err := client.SaveWorkflow(cmd.Context(), workspaceID, args[0], api.SaveWorkflowInput{Name: name, Description: description, ExpectedRevision: revision, Definition: file.Definition})
			if err != nil {
				return workflowConflictHint(err, "workflow", args[0], revision, "workflow get "+args[0])
			}
			return printWorkflowSummary(cfg, w)
		},
	}
	addWorkflowFileFlags(cmd)
	cmd.Flags().Int(workflowRevisionFlag, 0, "workflow revision this edit is based on")
	return cmd
}

// workflowConflictHint keeps the server's message and adds the recovery step
// when the server answered 409. The CLI never retries a conflicted write.
func workflowConflictHint(err error, noun, id string, revision int, reread string) error {
	var apiErr *api.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
		return fmt.Errorf("%w\nThe %s %s no longer matches revision %d, so nothing was changed. Read it again with `openpost %s`, then retry with the current revision", err, noun, id, revision, reread)
	}
	return err
}

func addRevisionFlag(cmd *cobra.Command, noun string) {
	cmd.Flags().Int(workflowRevisionFlag, 0, noun+" revision to act on (default: read the current revision)")
}

// workflowRevision uses --expected-revision, or reads the workflow's current
// revision. The server still rejects the write if it changed in between.
func workflowRevision(cmd *cobra.Command, client *api.Client, workspaceID, id string) (int, error) {
	if cmd.Flags().Changed(workflowRevisionFlag) {
		revision, _ := cmd.Flags().GetInt(workflowRevisionFlag)
		return revision, nil
	}
	current, err := client.GetWorkflow(cmd.Context(), workspaceID, id)
	if err != nil {
		return 0, err
	}
	return current.Revision, nil
}

func newWorkflowStateCmd(
	verb, short string,
	confirmFirst bool,
	act func(*api.Client, context.Context, string, string, int) (*api.Workflow, error),
	long string,
) *cobra.Command {
	cmd := &cobra.Command{
		Use:   verb + " <workflow-id>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if confirmFirst {
				ok, err := confirmWorkflowAction(cmd, fmt.Sprintf("%s workflow %s?", strings.ReplaceAll(verb, "-", " "), args[0]))
				if err != nil || !ok {
					return err
				}
			}
			cfg, client, workspaceID, err := workflowRuntime(cmd)
			if err != nil {
				return err
			}
			revision, err := workflowRevision(cmd, client, workspaceID, args[0])
			if err != nil {
				return err
			}
			w, err := act(client, cmd.Context(), workspaceID, args[0], revision)
			if err != nil {
				return workflowConflictHint(err, "workflow", args[0], revision, "workflow get "+args[0])
			}
			return printWorkflowSummary(cfg, w)
		},
	}
	addRevisionFlag(cmd, "workflow")
	return cmd
}

func newWorkflowDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <workflow-id>",
		Short: "Delete a paused workflow with no active runs",
		Long:  "Permanently delete a paused workflow that has no active runs, together with its run history. Posts it created remain.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ok, err := confirmWorkflowAction(cmd, fmt.Sprintf("Delete workflow %s and its run history?", args[0]))
			if err != nil || !ok {
				return err
			}
			cfg, client, workspaceID, err := workflowRuntime(cmd)
			if err != nil {
				return err
			}
			if _, err := client.DeleteWorkflow(cmd.Context(), workspaceID, args[0]); err != nil {
				return err
			}
			if cfg.AsJSON {
				return printerFrom(cfg).PrintJSON(map[string]string{"id": args[0], "status": "deleted"})
			}
			printerFrom(cfg).Printf("Deleted workflow %s.", args[0])
			return nil
		},
	}
}

func newWorkflowRunsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "runs [workflow-id]",
		Short: "List the latest runs with state and stop reason",
		Long:  "List the latest 50 runs, for one workflow or for the whole workspace. STOP REASON is the reason the server recorded when it ended or refused a run, such as a sender's daily limit. Use `workflow run <run-id>` for step results.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, workspaceID, err := workflowRuntime(cmd)
			if err != nil {
				return err
			}
			workflowID := ""
			if len(args) == 1 {
				workflowID = args[0]
			}
			runs, err := client.ListWorkflowRunSummaries(cmd.Context(), workspaceID, workflowID)
			if err != nil {
				return err
			}
			p := printerFrom(cfg)
			if cfg.AsJSON {
				return p.PrintJSON(runs)
			}
			rows := make([][]string, 0, len(runs))
			for _, run := range runs {
				rows = append(rows, []string{run.ID, run.WorkflowName, run.State, run.Mode, strconv.Itoa(run.WorkflowRevision), emptyDash(run.CreatedAt), emptyDash(run.Error)})
			}
			p.Table([]string{"RUN", "WORKFLOW", "STATE", "MODE", "WORKFLOW REV", "CREATED", "STOP REASON"}, rows)
			return nil
		},
	}
}

func newWorkflowRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <run-id>",
		Short: "Show a run's steps, effects and errors; approve or cancel it",
		Long:  "Show one run: its state, stop reason, and each step's state, error and recorded output. With --json the output is the complete run, including every step's inputs and output.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, workspaceID, err := workflowRuntime(cmd)
			if err != nil {
				return err
			}
			run, err := client.GetWorkflowRun(cmd.Context(), workspaceID, args[0])
			if err != nil {
				return err
			}
			return printWorkflowRun(cfg, run, true)
		},
	}
	cmd.AddCommand(newWorkflowRunApproveCmd(), newWorkflowRunCancelCmd())
	return cmd
}

func printWorkflowRun(cfg *config.Runtime, run *api.WorkflowRun, withSteps bool) error {
	p := printerFrom(cfg)
	if cfg.AsJSON {
		return p.PrintJSON(run)
	}
	p.Table([]string{"FIELD", "VALUE"}, [][]string{
		{"id", run.ID},
		{"workflow", run.WorkflowName + " (" + run.WorkflowID + ")"},
		{"workflow_revision", strconv.Itoa(run.WorkflowRevision)},
		{"mode", run.Mode},
		{"state", run.State},
		{"revision", strconv.Itoa(run.Revision)},
		{"current_step", emptyDash(run.CurrentStepID)},
		{"stop_reason", emptyDash(run.Error)},
		{"wake_at", emptyDash(run.WakeAt)},
		{"created_at", emptyDash(run.CreatedAt)},
		{"updated_at", emptyDash(run.UpdatedAt)},
	})
	if !withSteps || len(run.Steps) == 0 {
		return nil
	}
	rows := make([][]string, 0, len(run.Steps))
	for _, step := range run.Steps {
		detail := step.Error
		if reason, _ := step.Output["reason"].(string); detail == "" && reason != "" {
			detail = reason
		}
		rows = append(rows, []string{step.StepID, step.Kind, step.State, emptyDash(detail)})
	}
	p.Table([]string{"STEP", "KIND", "STATE", "ERROR OR REASON"}, rows)
	for _, step := range run.Steps {
		if len(step.Output) == 0 {
			continue
		}
		encoded, err := json.Marshal(step.Output)
		if err != nil {
			return err
		}
		p.Printf("output %s\t%s", step.StepID, encoded)
	}
	return nil
}

// workflowRunRevision uses --expected-revision, or reads the run's current
// revision. The run is returned when it was read.
func workflowRunRevision(cmd *cobra.Command, client *api.Client, workspaceID, id string) (*api.WorkflowRun, int, error) {
	if cmd.Flags().Changed(workflowRevisionFlag) {
		revision, _ := cmd.Flags().GetInt(workflowRevisionFlag)
		return nil, revision, nil
	}
	run, err := client.GetWorkflowRun(cmd.Context(), workspaceID, id)
	if err != nil {
		return nil, 0, err
	}
	return run, run.Revision, nil
}

func newWorkflowRunApproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approve <run-id>",
		Short: "Approve a run that is waiting for review",
		Long: "Approve a run waiting for review and continue it. Approving a post review needs --post-revision, the post revision you reviewed (see `openpost publication view`); " +
			"if the post changed since, the server refuses and nothing is approved. For a reviewed reply, --reply-text replaces the proposed text.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ok, err := confirmWorkflowAction(cmd, fmt.Sprintf("Approve workflow run %s and continue it?", args[0]))
			if err != nil || !ok {
				return err
			}
			cfg, client, workspaceID, err := workflowRuntime(cmd)
			if err != nil {
				return err
			}
			known, revision, err := workflowRunRevision(cmd, client, workspaceID, args[0])
			if err != nil {
				return err
			}
			input := api.ApproveWorkflowRunInput{ExpectedRevision: revision}
			if cmd.Flags().Changed("post-revision") {
				input.PublicationRevision, _ = cmd.Flags().GetInt("post-revision")
			} else if postID := postAwaitingReview(known); postID != "" {
				return fmt.Errorf("run %s waits for review of post %s: read it with `openpost publication view %s`, then pass the revision you reviewed as --post-revision", args[0], postID, postID)
			}
			if cmd.Flags().Changed("reply-text") {
				text, _ := cmd.Flags().GetString("reply-text")
				input.ReplyText = &text
			}
			run, err := client.ApproveWorkflowRun(cmd.Context(), workspaceID, args[0], input)
			if err != nil {
				return workflowConflictHint(err, "run", args[0], revision, "workflow run "+args[0])
			}
			return printWorkflowRun(cfg, run, false)
		},
	}
	addRevisionFlag(cmd, "run")
	cmd.Flags().Int("post-revision", 0, "post revision that was reviewed (required for post reviews)")
	cmd.Flags().String("reply-text", "", "replacement text for a reviewed comment or message reply")
	return cmd
}

// postAwaitingReview returns the post a run is waiting for a person to review,
// or "" when the run is not parked on a post approval or was not read.
func postAwaitingReview(run *api.WorkflowRun) string {
	if run == nil || run.State != "awaiting_approval" {
		return ""
	}
	for _, step := range run.Steps {
		if step.StepID == run.CurrentStepID && step.Kind == "approval" {
			id, _ := step.Inputs["publication_id"].(string)
			return id
		}
	}
	return ""
}

func newWorkflowRunCancelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel <run-id>",
		Short: "Cancel the remaining steps of a run",
		Long:  "Cancel the remaining steps of a run. An action already dispatched may finish, and drafts or queued posts it created remain.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ok, err := confirmWorkflowAction(cmd, fmt.Sprintf("Cancel the remaining steps of workflow run %s?", args[0]))
			if err != nil || !ok {
				return err
			}
			cfg, client, workspaceID, err := workflowRuntime(cmd)
			if err != nil {
				return err
			}
			_, revision, err := workflowRunRevision(cmd, client, workspaceID, args[0])
			if err != nil {
				return err
			}
			run, err := client.CancelWorkflowRun(cmd.Context(), workspaceID, args[0], revision)
			if err != nil {
				return workflowConflictHint(err, "run", args[0], revision, "workflow run "+args[0])
			}
			return printWorkflowRun(cfg, run, false)
		},
	}
	addRevisionFlag(cmd, "run")
	return cmd
}

func newWorkflowSampleSourceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sample-source <workflow-id>",
		Short: "Read example items from a workflow's source without side effects",
		Long:  "Read example items from the source of a workflow's draft. It advances no production cursor, starts no run and writes nothing to a provider.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, workspaceID, err := workflowRuntime(cmd)
			if err != nil {
				return err
			}
			w, err := client.GetWorkflow(cmd.Context(), workspaceID, args[0])
			if err != nil {
				return err
			}
			var definition struct {
				Source json.RawMessage `json:"source"`
			}
			if err := json.Unmarshal(w.Definition, &definition); err != nil || len(definition.Source) == 0 {
				return fmt.Errorf("workflow %s has no readable source", args[0])
			}
			items, err := client.SampleWorkflowSource(cmd.Context(), workspaceID, definition.Source)
			if err != nil {
				return err
			}
			p := printerFrom(cfg)
			if cfg.AsJSON {
				return p.PrintJSON(items)
			}
			rows := make([][]string, 0, len(items))
			for _, item := range items {
				rows = append(rows, []string{item.ID, emptyDash(preview(item.Title, 40)), emptyDash(preview(item.Body, 60)), emptyDash(item.URL)})
			}
			p.Table([]string{"ID", "TITLE", "BODY", "URL"}, rows)
			return nil
		},
	}
}

func newWorkflowTestNodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test-node <workflow-id> --step <step-id>",
		Short: "Run one data or tool step with supplied input data",
		Long:  "Run a single data or tool step of the saved draft with the JSON object you supply as its source data. Publication and reply steps are refused. The result is stored in run history.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			step, _ := cmd.Flags().GetString("step")
			data := map[string]any{}
			if path, _ := cmd.Flags().GetString("data"); path != "" {
				raw, err := readJSONInput(path)
				if err != nil {
					return err
				}
				if err := json.Unmarshal(raw, &data); err != nil {
					return fmt.Errorf("%s is not a JSON object: %w", path, err)
				}
			}
			cfg, client, workspaceID, err := workflowRuntime(cmd)
			if err != nil {
				return err
			}
			revision, err := workflowRevision(cmd, client, workspaceID, args[0])
			if err != nil {
				return err
			}
			run, err := client.TestWorkflowNode(cmd.Context(), workspaceID, args[0], api.TestWorkflowNodeInput{ExpectedRevision: revision, StepID: step, Data: data})
			if err != nil {
				return workflowConflictHint(err, "workflow", args[0], revision, "workflow get "+args[0])
			}
			return printWorkflowRun(cfg, run, true)
		},
	}
	cmd.Flags().String("step", "", "ID of the step to run")
	cmd.Flags().String("data", "", "JSON file with the input data object, or - for stdin")
	_ = cmd.MarkFlagRequired("step")
	addRevisionFlag(cmd, "workflow")
	return cmd
}
