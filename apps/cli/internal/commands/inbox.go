package commands

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/openpost/cli/internal/api"
	"github.com/openpost/cli/internal/output"
	"github.com/spf13/cobra"
)

// Inbox commands retain the complete server response, including native subject,
// moderation and operation metadata. Provider writes remain durable jobs.
func newInboxCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "inbox", Short: "Read and act on comments, incoming mentions and business reviews"}
	cmd.AddCommand(newInboxListCmd(), newInboxGetCmd(), newInboxRefreshCmd(), newInboxReadCmd(), newInboxStateCmd(), newInboxActionCmd())
	return cmd
}

type inboxRequest func(*cobra.Command, *api.Client, string) (json.RawMessage, error)

func runInboxRequest(request inboxRequest) func(*cobra.Command, []string) error {
	return runInboxRequestRendered(request, nil)
}

// runInboxRequestRendered prints the complete server response as JSON. A
// non-nil render replaces that output with a human view unless --json is set.
func runInboxRequestRendered(request inboxRequest, render func(*output.Printer, json.RawMessage) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		cfg, err := runtimeFrom(cmd)
		if err != nil {
			return err
		}
		client, err := clientFrom(cfg)
		if err != nil {
			return err
		}
		workspace, err := activeWorkspaceID(cmd, client)
		if err != nil {
			return err
		}
		result, err := request(cmd, client, workspace)
		if err != nil {
			return err
		}
		if len(result) == 0 {
			result = json.RawMessage(`{"updated":true}`)
		}
		p := printerFrom(cfg)
		if render != nil && !cfg.AsJSON {
			return render(p, result)
		}
		return p.PrintJSON(result)
	}
}

type inboxListItem struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	Platform        string `json:"platform"`
	AuthorName      string `json:"author_name"`
	AuthorHandle    string `json:"author_handle"`
	Body            string `json:"body"`
	ReadAt          string `json:"read_at"`
	ArchivedAt      string `json:"archived_at"`
	LastSeenAt      string `json:"last_seen_at"`
	RemoteCreatedAt string `json:"remote_created_at"`
}

func renderInboxList(p *output.Printer, result json.RawMessage) error {
	var page struct {
		Items      []inboxListItem `json:"items"`
		Total      int             `json:"total"`
		NextCursor string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(result, &page); err != nil {
		return fmt.Errorf("decode Inbox response: %w", err)
	}
	if len(page.Items) == 0 {
		p.Printf("No Inbox items.")
		return nil
	}
	rows := make([][]string, 0, len(page.Items))
	for _, item := range page.Items {
		state := "unread"
		switch {
		case item.ArchivedAt != "":
			state = "archived"
		case item.ReadAt != "":
			state = "read"
		}
		author := item.AuthorName
		if author == "" {
			author = item.AuthorHandle
		}
		when := item.RemoteCreatedAt
		if when == "" {
			when = item.LastSeenAt
		}
		rows = append(rows, []string{item.ID, emptyDash(item.Kind), emptyDash(item.Platform), emptyDash(author), state, emptyDash(when), emptyDash(preview(item.Body, 60))})
	}
	p.Table([]string{"ID", "KIND", "PLATFORM", "AUTHOR", "STATE", "WHEN", "BODY"}, rows)
	p.Printf("%d of %d items shown.", len(page.Items), page.Total)
	if page.NextCursor != "" {
		p.Printf("More items: pass --cursor %s", page.NextCursor)
	}
	return nil
}

func inboxFilterFlags(cmd *cobra.Command) {
	cmd.Flags().String("kind", "", "comment, mention or review; omit for all")
	cmd.Flags().String("account-id", "", "connected account ID")
	cmd.Flags().String("platform", "", "filter by provider")
	cmd.Flags().String("publication-id", "", "filter by an OpenPost post, excluding unrelated native activity")
	cmd.Flags().Bool("archived", false, "read archived items instead of active items")
}

func inboxFilters(cmd *cobra.Command, workspace string) (map[string]any, error) {
	filters := map[string]any{"workspace_id": workspace}
	for _, flag := range []string{"kind", "account-id", "platform", "publication-id"} {
		value, _ := cmd.Flags().GetString(flag)
		if flag == "kind" && value != "" && value != "comment" && value != "mention" && value != "review" {
			return nil, fmt.Errorf("--kind must be comment, mention or review")
		}
		if value != "" {
			filters[strings.ReplaceAll(flag, "-", "_")] = value
		}
	}
	filters["archived"], _ = cmd.Flags().GetBool("archived")
	return filters, nil
}

func newInboxListCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "Read stored Inbox items and feed health, without provider calls", Args: cobra.NoArgs}
	inboxFilterFlags(cmd)
	cmd.Flags().Bool("unread", false, "only unread items")
	cmd.Flags().Int("limit", 50, "page size, from 1 to 100")
	cmd.Flags().String("cursor", "", "next_cursor from the previous response")
	cmd.RunE = runInboxRequestRendered(func(cmd *cobra.Command, client *api.Client, workspace string) (json.RawMessage, error) {
		filters, err := inboxFilters(cmd, workspace)
		if err != nil {
			return nil, err
		}
		limit, _ := cmd.Flags().GetInt("limit")
		if limit < 1 || limit > 100 {
			return nil, fmt.Errorf("--limit must be from 1 to 100")
		}
		query := url.Values{}
		for key, value := range filters {
			query.Set(key, fmt.Sprint(value))
		}
		query.Set("limit", strconv.Itoa(limit))
		cursor, _ := cmd.Flags().GetString("cursor")
		query.Set("cursor", cursor)
		unread, _ := cmd.Flags().GetBool("unread")
		query.Set("unread_only", strconv.FormatBool(unread))
		var result json.RawMessage
		err = client.GetJSON(cmd.Context(), "/api/v1/engagement?"+query.Encode(), &result)
		return result, err
	}, renderInboxList)
	return cmd
}

func newInboxGetCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "get <item-id>", Short: "Read an Inbox item, native context and review reply receipt", Args: cobra.ExactArgs(1)}
	cmd.RunE = runInboxRequest(func(cmd *cobra.Command, client *api.Client, workspace string) (json.RawMessage, error) {
		var result json.RawMessage
		err := client.GetJSON(cmd.Context(), "/api/v1/engagement/"+url.PathEscape(cmd.Flags().Args()[0])+"?workspace_id="+url.QueryEscape(workspace), &result)
		return result, err
	})
	return cmd
}

func newInboxRefreshCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "refresh", Short: "Queue durable collection for enabled accounts", Args: cobra.NoArgs}
	cmd.RunE = runInboxRequest(func(cmd *cobra.Command, client *api.Client, workspace string) (json.RawMessage, error) {
		var result json.RawMessage
		err := client.PostJSON(cmd.Context(), "/api/v1/engagement/refresh", map[string]string{"workspace_id": workspace}, &result)
		return result, err
	})
	return cmd
}

func newInboxReadCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "mark-read", Short: "Mark every matching unread item read across all pages", Args: cobra.NoArgs}
	inboxFilterFlags(cmd)
	cmd.RunE = runInboxRequest(func(cmd *cobra.Command, client *api.Client, workspace string) (json.RawMessage, error) {
		filters, err := inboxFilters(cmd, workspace)
		if err != nil {
			return nil, err
		}
		var result json.RawMessage
		err = client.PostJSON(cmd.Context(), "/api/v1/engagement/read", filters, &result)
		return result, err
	})
	return cmd
}

func newInboxStateCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "state <item-id>...", Short: "Update shared Workspace read and archive state", Args: cobra.RangeArgs(1, 100)}
	cmd.Flags().Bool("read", false, "mark read, or --read=false to mark unread")
	cmd.Flags().Bool("archived", false, "archive, or --archived=false to reopen")
	cmd.RunE = runInboxRequest(func(cmd *cobra.Command, client *api.Client, workspace string) (json.RawMessage, error) {
		body := map[string]any{"workspace_id": workspace, "ids": cmd.Flags().Args()}
		for _, flag := range []string{"read", "archived"} {
			if cmd.Flags().Changed(flag) {
				body[flag], _ = cmd.Flags().GetBool(flag)
			}
		}
		if len(body) == 2 {
			return nil, fmt.Errorf("--read or --archived is required")
		}
		var result json.RawMessage
		err := client.PostJSON(cmd.Context(), "/api/v1/engagement/state", body, &result)
		return result, err
	})
	return cmd
}

func newInboxActionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "act <item-id>", Short: "Queue a supported reply or moderation action", Args: cobra.ExactArgs(1), Long: "Queue an action supported by the item's can_* capabilities. For a Google review, reply replaces the owner reply and delete_reply removes only that reply. Reviews cannot be deleted. Inspect the returned job and the item's moderation state before replacing a review reply again. Reuse --operation-id only for the same intent."}
	cmd.Flags().String("action", "reply", "reply, delete_reply, hide, delete, like or unlike")
	cmd.Flags().String("message", "", "reply text; Google owner replies allow 4096 UTF-8 bytes")
	cmd.Flags().String("operation-id", "", "stable ID for this exact intent, required for safe retries")
	_ = cmd.MarkFlagRequired("operation-id")
	cmd.RunE = runInboxRequest(func(cmd *cobra.Command, client *api.Client, workspace string) (json.RawMessage, error) {
		action, _ := cmd.Flags().GetString("action")
		if !map[string]bool{"reply": true, "delete_reply": true, "hide": true, "delete": true, "like": true, "unlike": true}[action] {
			return nil, fmt.Errorf("unsupported --action")
		}
		message, _ := cmd.Flags().GetString("message")
		operation, _ := cmd.Flags().GetString("operation-id")
		if strings.TrimSpace(operation) == "" || len(operation) > 128 {
			return nil, fmt.Errorf("--operation-id must have 1 to 128 bytes")
		}
		if action == "reply" && strings.TrimSpace(message) == "" {
			return nil, fmt.Errorf("--message is required for reply")
		}
		var result json.RawMessage
		err := client.PostJSON(cmd.Context(), "/api/v1/engagement/"+url.PathEscape(cmd.Flags().Args()[0])+"/actions", map[string]any{"workspace_id": workspace, "action": action, "message": message, "client_operation_id": operation}, &result)
		return result, err
	})
	return cmd
}
