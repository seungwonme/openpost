package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/openpost/cli/internal/accountpicker"
	"github.com/openpost/cli/internal/api"
	"github.com/openpost/cli/internal/config"
	"github.com/openpost/cli/internal/schedule"
)

type postFlags struct {
	segmentsFile string
	content      string
	file         string
	accounts     string
	schedule     string
	media        []string
	mediaAlt     []string
	threadDraft  string
	randomDelay  int
	limit        int
	offset       int
	status       string
}

func newPostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "post",
		Short: "Create, list, view, update, and delete posts",
		Long:  "Create, list, view, update, and delete posts. The post command is the friendly name for Publication workflows; every command creates, reads, and mutates Publications and Renditions through the canonical API.",
	}
	cmd.AddCommand(newPostCreateCmd())
	cmd.AddCommand(newPostListCmd())
	cmd.AddCommand(newPostViewCmd())
	cmd.AddCommand(newPostUpdateCmd())
	cmd.AddCommand(newPostDeleteCmd())
	cmd.AddCommand(newPublicationScheduleCmd())
	return cmd
}

func newPostCreateCmd() *cobra.Command {
	var flags postFlags
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a draft or scheduled post",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, client, workspaceID, settings, err := postRuntime(cmd)
			if err != nil {
				return err
			}
			content, err := contentFromFlags(flags.content, flags.file)
			if err != nil {
				return err
			}
			segments, err := readOptionalPublicationSegments(flags.segmentsFile)
			if err != nil {
				return err
			}
			if len(segments) > 0 {
				content = segments[0].Body
			}
			accountIDs, err := resolveAccounts(cmd, client, workspaceID, flags.accounts)
			if err != nil {
				return err
			}
			scheduledAt, label, err := parseScheduleFlag(cmd, client, workspaceID, flags.schedule, settings.Timezone)
			if err != nil {
				return err
			}
			if err := confirmNaturalSchedule(cfg.Yes, scheduledAt, label); err != nil {
				return err
			}
			mediaIDs, err := resolveMedia(cmd, client, workspaceID, flags.media, flags.mediaAlt)
			if err != nil {
				return err
			}
			intent := "post"
			profile := "short_text"
			if flags.threadDraft != "" || len(segments) > 1 {
				intent = "thread"
				profile = "thread"
			}
			var randomDelay *int
			if cmd.Flags().Changed("random-delay") {
				randomDelay = &flags.randomDelay
			}
			publication, err := client.CreatePublication(cmd.Context(), api.CreatePublicationInput{
				Segments:           segments,
				WorkspaceID:        workspaceID,
				Title:              defaultString(firstLineCLI(content), "Untitled post"),
				Intent:             intent,
				CreationPreset:     intent,
				ContentProfile:     profile,
				SourceText:         content,
				ScheduledAt:        scheduledAt,
				RandomDelayMinutes: randomDelay,
				SocialAccountIDs:   accountIDs,
				Media:              publicationMediaInputs(mediaIDs),
			})
			if err != nil {
				return err
			}
			if scheduledAt != nil {
				if _, err := client.SchedulePublication(cmd.Context(), publication.ID, publication.Revision); err != nil {
					return fmt.Errorf("draft created (publication %s), but scheduling failed: %w\nRetry scheduling the existing publication: openpost post schedule %s --at %s\nDo not rerun post create; it creates another draft", publication.ID, err, publication.ID, scheduledAt.Format(time.RFC3339Nano))
				}
				publicationID := publication.ID
				publication, err = client.GetPublication(cmd.Context(), publicationID)
				if err != nil {
					return fmt.Errorf("publication %s created and scheduling accepted, but fetching its status failed: %w\nCheck it with: openpost post view %s", publicationID, err, publicationID)
				}
			}
			return printPostPublicationSummary(cfg, publication)
		},
	}
	addCreatePostFlags(cmd, &flags)
	return cmd
}

func newPostListCmd() *cobra.Command {
	var flags postFlags
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List posts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, client, workspaceID, _, err := postRuntime(cmd)
			if err != nil {
				return err
			}
			publications, err := client.ListPublications(cmd.Context(), api.ListPublicationsInput{WorkspaceID: workspaceID, Status: flags.status, Limit: flags.limit, Offset: flags.offset})
			if err != nil {
				return err
			}
			p := printerFrom(cfg)
			if cfg.AsJSON {
				return p.PrintJSON(publications)
			}
			rows := make([][]string, 0, len(publications))
			for _, publication := range publications {
				rows = append(rows, []string{
					publication.ID,
					publication.Status,
					scheduleLabel(publication.ScheduledAt),
					preview(publication.SourceText, 80),
					strconv.Itoa(len(publication.Renditions)),
				})
			}
			p.Table([]string{"ID", "STATUS", "SCHEDULED", "CONTENT", "ACCOUNTS"}, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.status, "status", "", "filter by status: draft, scheduled, published, failed")
	cmd.Flags().IntVar(&flags.limit, "limit", 0, "maximum number of posts to return")
	cmd.Flags().IntVar(&flags.offset, "offset", 0, "number of posts to skip")
	return cmd
}

func newPostViewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "view <post-id>",
		Short: "View a post",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := runtimeFrom(cmd)
			if err != nil {
				return err
			}
			client, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			publication, err := client.GetPublication(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			p := printerFrom(cfg)
			if cfg.AsJSON {
				return p.PrintJSON(publication)
			}
			p.Table([]string{"FIELD", "VALUE"}, [][]string{
				{"id", publication.ID},
				{"workspace_id", publication.WorkspaceID},
				{"created_by", publication.CreatedBy},
				{"status", publication.Status},
				{"scheduled_at", scheduleLabel(publication.ScheduledAt)},
				{"actual_run_at", emptyDash(publication.ActualRunAt)},
				{"created_at", emptyDash(publication.CreatedAt)},
				{"content", publication.SourceText},
				{"media_count", strconv.Itoa(len(publication.Media))},
				{"destination_count", strconv.Itoa(len(publication.Renditions))},
			})
			for _, rendition := range publication.Renditions {
				p.Printf("rendition %s\t%s\t%s\t%s", rendition.SocialAccountID, rendition.Platform, rendition.Status, emptyDash(rendition.ErrorMessage))
			}
			return nil
		},
	}
}

func newPostUpdateCmd() *cobra.Command {
	var flags postFlags
	cmd := &cobra.Command{
		Use:   "update <post-id>",
		Short: "Update a draft or scheduled post",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, workspaceID, settings, err := postRuntime(cmd)
			if err != nil {
				return err
			}
			current, err := client.GetPublication(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			segments, err := readOptionalPublicationSegments(flags.segmentsFile)
			if err != nil {
				return err
			}
			in := api.UpdatePublicationInput{ExpectedRevision: current.Revision, Segments: segments}
			changed := len(segments) > 0
			if cmd.Flags().Changed("content") {
				in.SourceText = &flags.content
				title := defaultString(firstLineCLI(flags.content), "Untitled post")
				in.Title = &title
				changed = true
			}
			if cmd.Flags().Changed("accounts") {
				accountIDs, err := resolveAccounts(cmd, client, workspaceID, flags.accounts)
				if err != nil {
					return err
				}
				in.Renditions, err = retainedRenditionsForAccounts(accountIDs, current)
				if err != nil {
					return err
				}
				changed = true
			}
			if cmd.Flags().Changed("schedule") {
				if flags.schedule == "" || strings.EqualFold(strings.TrimSpace(flags.schedule), "draft") {
					in.ClearSchedule = true
				} else {
					t, label, err := parseScheduleFlag(cmd, client, workspaceID, flags.schedule, settings.Timezone)
					if err != nil {
						return err
					}
					if t == nil {
						return fmt.Errorf("--schedule must resolve to a future time or draft")
					}
					if err := confirmNaturalSchedule(cfg.Yes, t, label); err != nil {
						return err
					}
					in.ScheduledAt = t
				}
				changed = true
			}
			if cmd.Flags().Changed("random-delay") {
				in.RandomDelayMinutes = &flags.randomDelay
				changed = true
			}
			if cmd.Flags().Changed("media") {
				mediaIDs, err := resolveMedia(cmd, client, workspaceID, flags.media, flags.mediaAlt)
				if err != nil {
					return err
				}
				if len(in.Segments) == 0 {
					in.Segments = canonicalSegmentInputs(current)
				}
				in.Segments[0].Media = publicationMediaInputs(mediaIDs)
				if in.SourceText != nil {
					in.Segments[0].Body = *in.SourceText
					delete(in.Segments[0].Settings, "mentions")
				}
				changed = true
			}
			if !changed {
				return fmt.Errorf("at least one update flag is required")
			}
			publication, err := client.UpdatePublication(cmd.Context(), args[0], in)
			if err != nil {
				return err
			}
			return printPostPublicationSummary(cfg, publication)
		},
	}
	cmd.Flags().StringVar(&flags.content, "content", "", "post content")
	cmd.Flags().StringVar(&flags.segmentsFile, "segments-file", "", "Ordered segment JSON with settings.mentions and UTF-8 byte ranges; use - for stdin")
	cmd.Flags().StringVar(&flags.schedule, "schedule", "", "natural-language, RFC3339, next-slot, now, or draft; empty string unschedules")
	cmd.Flags().StringVar(&flags.accounts, "accounts", "", "comma-separated account selectors")
	cmd.Flags().IntVar(&flags.randomDelay, "random-delay", 0, "random delay in minutes")
	cmd.Flags().StringArrayVar(&flags.media, "media", nil, "replacement media id or local file path; repeatable; pass an empty value to clear")
	cmd.Flags().StringArrayVar(&flags.mediaAlt, "media-alt", nil, "alt text for the matching uploaded --media")
	cmd.MarkFlagsMutuallyExclusive("segments-file", "content")
	return cmd
}

func newPostDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <post-id>",
		Short: "Delete a draft or scheduled post",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := runtimeFrom(cmd)
			if err != nil {
				return err
			}
			client, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			ok, err := confirmMutation(cfg, fmt.Sprintf("Delete post %s?", args[0]))
			if err != nil {
				return err
			}
			if !ok {
				printerFrom(cfg).Printf("Canceled.")
				return nil
			}
			publication, err := client.GetPublication(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if _, err := client.DeletePublication(cmd.Context(), args[0], publication.Revision); err != nil {
				return err
			}
			p := printerFrom(cfg)
			if cfg.AsJSON {
				return p.PrintJSON(map[string]string{"id": args[0], "status": "deleted"})
			}
			p.Printf("Deleted post %s.", args[0])
			return nil
		},
	}
}

func addCreatePostFlags(cmd *cobra.Command, flags *postFlags) {
	cmd.Flags().StringVar(&flags.content, "content", "", "post content")
	cmd.Flags().StringVar(&flags.segmentsFile, "segments-file", "", "Ordered segment JSON with settings.mentions and UTF-8 byte ranges; use - for stdin")
	cmd.Flags().StringVar(&flags.file, "file", "", "read post content from a file")
	cmd.Flags().StringVar(&flags.accounts, "accounts", "", "comma-separated account selectors")
	cmd.Flags().StringVar(&flags.schedule, "schedule", "", "natural-language, RFC3339, next-slot, now, or draft")
	cmd.Flags().StringArrayVar(&flags.media, "media", nil, "media id or local file path; repeatable")
	cmd.Flags().StringArrayVar(&flags.mediaAlt, "media-alt", nil, "alt text for the matching uploaded --media")
	cmd.Flags().StringVar(&flags.threadDraft, "thread-draft", "", "encoded thread draft to attach")
	cmd.Flags().IntVar(&flags.randomDelay, "random-delay", 0, "random delay in minutes")
	cmd.MarkFlagsMutuallyExclusive("segments-file", "content", "file", "thread-draft")
}

func postRuntime(cmd *cobra.Command) (*config.Runtime, *api.Client, string, *api.WorkspaceSettings, error) {
	cfg, err := runtimeFrom(cmd)
	if err != nil {
		return nil, nil, "", nil, err
	}
	client, err := clientFrom(cfg)
	if err != nil {
		return nil, nil, "", nil, err
	}
	workspaceID, err := activeWorkspaceID(cmd, client)
	if err != nil {
		return nil, nil, "", nil, err
	}
	settings, err := client.GetWorkspaceSettings(cmd.Context(), workspaceID)
	if err != nil {
		return nil, nil, "", nil, err
	}
	return cfg, client, workspaceID, settings, nil
}

func contentFromFlags(content, file string) (string, error) {
	if file == "" {
		return content, nil
	}
	if file == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r"), nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r"), nil
}

func parseScheduleFlag(cmd *cobra.Command, client *api.Client, workspaceID, raw, workspaceTZ string) (*time.Time, string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, "", nil
	}
	if isNextSlotSchedule(raw) {
		out, err := client.NextAvailableSlot(cmd.Context(), api.NextAvailableSlotInput{WorkspaceID: workspaceID})
		if err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(out.SlotTime) == "" {
			return nil, "", fmt.Errorf("no next posting slot available: %s", out.Message)
		}
		t, err := time.Parse(time.RFC3339, out.SlotTime)
		if err != nil {
			return nil, "", fmt.Errorf("server returned invalid next slot %q: %w", out.SlotTime, err)
		}
		return &t, "slot", nil
	}
	cfg, err := runtimeFrom(cmd)
	if err != nil {
		return nil, "", err
	}
	loc := schedule.MustLoadLocation(cfg.Profile.Timezone)
	res, err := schedule.Parse(raw, schedule.Options{Now: time.Now(), DefaultLocation: loc, WorkspaceTimezone: workspaceTZ})
	if err != nil {
		return nil, "", err
	}
	if res.Time.IsZero() {
		return nil, "", nil
	}
	return &res.Time, res.Source, nil
}

func isNextSlotSchedule(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "next-slot", "next slot", "slot":
		return true
	default:
		return false
	}
}

func confirmNaturalSchedule(skip bool, t *time.Time, source string) error {
	if skip || t == nil || source != "natural" {
		return nil
	}
	ok, err := askYesNo(fmt.Sprintf("Schedule for %s? [Y/n] ", t.Format("Jan 02 2006 15:04 MST")), true)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("cancelled")
	}
	return nil
}

func askYesNo(prompt string, def bool) (bool, error) {
	fmt.Fprint(os.Stderr, prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return def, nil
	}
	return line == "y" || line == "yes", nil
}

func resolveAccounts(cmd *cobra.Command, client *api.Client, workspaceID, csv string) ([]string, error) {
	selectors := splitCSV(csv)
	if len(selectors) == 0 {
		return nil, nil
	}
	accounts, err := client.ListAccounts(cmd.Context(), workspaceID)
	if err != nil {
		return nil, err
	}
	return accountpicker.Resolve(workspaceID, selectors, accounts)
}

func resolveMedia(cmd *cobra.Command, client *api.Client, workspaceID string, mediaValues, altValues []string) ([]string, error) {
	inputs := resolveMediaInputs(mediaValues, altValues)
	existing := map[string]bool{}
	list, _ := client.ListMedia(cmd.Context(), workspaceID, 200)
	for _, item := range list {
		existing[item.ID] = true
	}
	out := make([]string, 0, len(inputs))
	for _, input := range inputs {
		value := input.value
		if strings.HasPrefix(value, "med_") || existing[value] {
			out = append(out, value)
			continue
		}
		if input.localFile {
			media, err := client.UploadMedia(cmd.Context(), workspaceID, value, input.alt)
			if err != nil {
				return nil, err
			}
			out = append(out, media.ID)
			continue
		}
		return nil, fmt.Errorf("media %q is not an existing file or known media id", value)
	}
	return out, nil
}

type mediaInput struct {
	value     string
	alt       string
	localFile bool
}

func resolveMediaInputs(mediaValues, altValues []string) []mediaInput {
	inputs := make([]mediaInput, 0, len(mediaValues))
	for i, value := range mediaValues {
		if st, err := os.Stat(value); err == nil && !st.IsDir() {
			alt := ""
			if i < len(altValues) {
				alt = altValues[i]
			}
			inputs = append(inputs, mediaInput{value: value, alt: alt, localFile: true})
			continue
		}
		for _, item := range splitCSV(value) {
			st, err := os.Stat(item)
			inputs = append(inputs, mediaInput{
				value:     item,
				localFile: err == nil && !st.IsDir(),
			})
		}
	}
	return inputs
}

func splitCSV(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	raw := strings.Split(csv, ",")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s := strings.TrimSpace(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func printPostPublicationSummary(cfg *config.Runtime, publication *api.Publication) error {
	p := printerFrom(cfg)
	if cfg.AsJSON {
		return p.PrintJSON(publication)
	}
	p.Table([]string{"ID", "STATUS", "SCHEDULED", "ACCOUNTS", "MEDIA"}, [][]string{{
		publication.ID,
		publication.Status,
		scheduleLabel(publication.ScheduledAt),
		strconv.Itoa(len(publication.Renditions)),
		strconv.Itoa(len(publication.Media)),
	}})
	return nil
}

func publicationMediaInputs(mediaIDs []string) []api.PublicationMediaInput {
	media := make([]api.PublicationMediaInput, 0, len(mediaIDs))
	for _, id := range mediaIDs {
		if strings.TrimSpace(id) != "" {
			media = append(media, api.PublicationMediaInput{MediaID: id})
		}
	}
	return media
}

func canonicalSegmentInputs(current *api.Publication) []api.PublicationSegmentInput {
	out := make([]api.PublicationSegmentInput, 0, len(current.Segments))
	for _, segment := range current.Segments {
		settings := make(map[string]any, len(segment.Settings))
		for key, value := range segment.Settings {
			settings[key] = value
		}
		out = append(out, api.PublicationSegmentInput{ID: segment.ID, Body: segment.Body, Title: segment.Title, Description: segment.Description, URL: segment.URL, Settings: settings, Media: authoredMediaInputs(segment.Media)})
	}
	if len(out) == 0 {
		out = append(out, api.PublicationSegmentInput{Body: current.SourceText, Media: authoredMediaInputs(current.Media)})
	}
	return out
}

func authoredMediaInputs(media []api.MediaSummary) []api.PublicationMediaInput {
	out := make([]api.PublicationMediaInput, 0, len(media))
	for _, item := range media {
		out = append(out, api.PublicationMediaInput{MediaID: item.ID, Role: item.Role, AltText: item.AltText, ThumbnailTimestampMS: item.ThumbnailTimestampMS, Settings: item.Settings})
	}
	return out
}

func retainedRenditionsForAccounts(accountIDs []string, current *api.Publication) ([]api.RenditionInput, error) {
	out := make([]api.RenditionInput, 0, len(current.Renditions))
	for _, accountID := range accountIDs {
		found := false
		for _, rendition := range current.Renditions {
			if rendition.SocialAccountID != accountID {
				continue
			}
			found = true
			input, err := authoredRenditionInput(rendition, current.Segments)
			if err != nil {
				return nil, err
			}
			out = append(out, input)
		}
		if !found {
			out = append(out, api.RenditionInput{SocialAccountID: accountID})
		}
	}
	return out, nil
}

func authoredRenditionInput(rendition api.Rendition, canonical []api.PublicationSegment) (api.RenditionInput, error) {
	out := api.RenditionInput{SocialAccountID: rendition.SocialAccountID, TargetKey: rendition.TargetKey, Profile: rendition.Profile, OutputProfile: rendition.OutputProfile, FormatLocked: rendition.FormatLocked, Settings: rendition.Settings}
	if rendition.ScheduleOverride != "" {
		parsed, err := time.Parse(time.RFC3339Nano, rendition.ScheduleOverride)
		if err != nil {
			return out, fmt.Errorf("invalid stored variant schedule: %w", err)
		}
		out.ScheduleOverride = &parsed
	}
	for _, segment := range rendition.Segments {
		if strings.HasPrefix(segment.ID, "legacy:") {
			out.Body = rendition.Body
			out.Title = rendition.Title
			out.Description = rendition.Description
			out.Media = authoredMediaInputs(rendition.Media)
			continue
		}
		inherited := segment.MediaInherited
		bodyOverride := retainedSegmentBody(segment, rendition, canonical)
		out.Segments = append(out.Segments, api.RenditionSegmentInput{PublicationSegmentID: segment.PublicationSegmentID, BodyOverride: bodyOverride, TitleOverride: segment.TitleOverride, DescriptionOverride: segment.DescriptionOverride, URLOverride: segment.URLOverride, Settings: segment.Settings, SourceOverrides: segment.SourceOverrides, MediaInherited: &inherited, Media: authoredMediaInputs(segment.Media)})
	}
	return out, nil
}

// scheduleLabel renders a scheduled_at value. A publication with no schedule
// shows "-" like every other empty table cell; its STATUS column already says
// whether it is a draft or has been published.
func scheduleLabel(s string) string {
	if strings.TrimSpace(s) == "" || strings.HasPrefix(s, "0001-01-01") {
		return "-"
	}
	return s
}

func preview(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

// Legacy ordinary variants may own text without a persisted override. Preserve
// that ownership when retaining accounts, before a canonical edit is applied.
func retainedSegmentBody(segment api.RenditionSegment, rendition api.Rendition, canonical []api.PublicationSegment) *string {
	if segment.BodyOverride != nil || len(segment.SourceOverrides) > 0 || (len(rendition.Segments) == 1 && len(canonical) > 1) {
		return segment.BodyOverride
	}
	for _, source := range canonical {
		if source.ID != segment.PublicationSegmentID {
			continue
		}
		// Generated poll/link text belongs to their projection owners.
		if source.Settings["poll"] != nil || source.Settings["link"] != nil || source.Body == segment.Body {
			return nil
		}
		body := segment.Body
		return &body
	}
	return nil
}
