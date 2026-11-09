package engagement

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/notifications"
	"github.com/openpost/backend/internal/services/workflows"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/stretchr/testify/require"
)

// nativeCommentProvider stands in for the Instagram adapter at its reader
// boundary. Wire formats are covered by the platform adapter tests.
type nativeCommentProvider struct {
	moderationProvider
	media      []platform.InstagramMediaSummary
	comments   map[string][]platform.Comment
	mediaLists int
	reads      []string
}

func (*nativeCommentProvider) EngagementSupport() platform.EngagementSupport {
	return platform.EngagementSupport{Enabled: true, CanReply: true, RequiredScopes: []string{"instagram_manage_comments"}}
}
func (*nativeCommentProvider) AccountActivitySupport() []platform.AccountActivitySupport {
	return []platform.AccountActivitySupport{{Feed: platform.InstagramCommentsFeed, RequiredScopes: []string{"instagram_basic", "instagram_manage_comments"}}}
}
func (*nativeCommentProvider) ListAccountActivity(context.Context, string, string, platform.AccountActivityRequest) (platform.AccountActivityPage, error) {
	panic("instagram comments are not paged account activity")
}
func (p *nativeCommentProvider) ListRecentMedia(_ context.Context, _, accountID string) ([]platform.InstagramMediaSummary, error) {
	if accountID != "remote" {
		return nil, fmt.Errorf("unexpected account %q", accountID)
	}
	p.mediaLists++
	return p.media, nil
}
func (p *nativeCommentProvider) ListComments(_ context.Context, _, _, mediaID string) ([]platform.Comment, error) {
	p.reads = append(p.reads, mediaID)
	return p.comments[mediaID], nil
}
func (p *nativeCommentProvider) ReadWebhookComment(_ context.Context, _, _, commentID, mediaID string) (platform.Comment, string, error) {
	for _, comment := range p.comments[mediaID] {
		if comment.ID == commentID {
			return comment, "https://www.instagram.com/reel/native/", nil
		}
	}
	return platform.Comment{}, "", fmt.Errorf("unknown comment %q", commentID)
}

func nativeCommentService(t *testing.T, scopes string) (*Service, *nativeCommentProvider) {
	t.Helper()
	s := admissionTestService(t)
	s.notifications = notifications.NewService(s.db)
	s.tokenSource = moderationTokens{}
	now := time.Now().UTC()
	_, err := s.db.NewInsert().Model(&models.AccountFeature{SocialAccountID: "a", WorkspaceID: "w", Feature: "engagement", Enabled: true, DecidedAt: now}).Exec(t.Context())
	require.NoError(t, err)
	_, err = s.db.NewUpdate().Model((*models.SocialAccount)(nil)).Set("platform='instagram', granted_scopes=?", scopes).Where("id='a'").Exec(t.Context())
	require.NoError(t, err)
	// The fixture post was published through OpenPost; owned-post collection reads it.
	_, err = s.db.NewUpdate().Model((*models.Rendition)(nil)).Set("platform='instagram', target_key='instagram', external_id='owned-media'").Where("id='r'").Exec(t.Context())
	require.NoError(t, err)
	provider := &nativeCommentProvider{comments: map[string][]platform.Comment{}}
	s.SetProvider("instagram", provider)
	return s, provider
}

func instagramComment(id, author, body string, created time.Time) platform.Comment {
	return platform.Comment{ID: id, AuthorID: author, AuthorName: author, Text: body, CreatedAt: created.Format(time.RFC3339), CanReply: true, CanHide: true, CanDelete: true}
}

func nativeCommentState(t *testing.T, s *Service) models.EngagementAccountSyncState {
	t.Helper()
	var state models.EngagementAccountSyncState
	require.NoError(t, s.db.NewSelect().Model(&state).Where("social_account_id='a' AND feed=?", platform.InstagramCommentsFeed).Scan(t.Context()))
	return state
}

func TestPolledNativeInstagramCommentReachesKeywordWorkflowSource(t *testing.T) {
	s, provider := nativeCommentService(t, "instagram_basic,instagram_manage_comments")
	now := time.Now().UTC()
	provider.media = []platform.InstagramMediaSummary{
		{ID: "native-reel", Permalink: "https://www.instagram.com/reel/native/", PublishedAt: now.Add(-time.Hour), CommentsCount: 2},
		{ID: "owned-media", PublishedAt: now.Add(-2 * time.Hour), CommentsCount: 5},
		{ID: "quiet-media", PublishedAt: now.Add(-3 * time.Hour), CommentsCount: 0},
	}
	provider.comments["native-reel"] = []platform.Comment{
		instagramComment("native-new", "reader", "GRIND please", now.Add(time.Minute)),
		instagramComment("native-old", "earlier", "nice reel", now.Add(-48*time.Hour)),
	}
	provider.comments["owned-media"] = []platform.Comment{instagramComment("owned-comment", "reader", "GRIND", now.Add(time.Minute))}

	executeAccountActivity(t, s, accountActivityReceipt(t, s))

	require.Equal(t, 1, provider.mediaLists)
	require.Equal(t, []string{"native-reel"}, provider.reads, "OpenPost posts and media without comments need no comment read")
	var rows []models.EngagementItem
	require.NoError(t, s.db.NewSelect().Model(&rows).Where("social_account_id='a' AND remote_id LIKE 'native-%'").Order("remote_id").Scan(t.Context()))
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Empty(t, row.RenditionID)
		require.Equal(t, "comment", row.Kind)
		require.Equal(t, "native-reel", row.ConversationRemoteID)
		require.JSONEq(t, `{"kind":"comment","remote_id":"native-reel","url":"https://www.instagram.com/reel/native/"}`, row.SubjectJSON)
	}
	state := nativeCommentState(t, s)
	require.Equal(t, "ok", state.Status)
	require.WithinDuration(t, time.Now().Add(nativeCommentCadence), state.NextSyncAt, 10*time.Second)

	items, err := workflows.NewService(s.db, nil, nil).Sample(t.Context(), workspaceaccess.ActorFacts{UserID: "u"}, "w",
		workflows.Source{Kind: "comment_received", AccountIDs: []string{"a"}, Keywords: []string{"GRIND"}, MatchMode: "word", DailyLimit: 20})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "native-new", rows[0].RemoteID)
	require.Equal(t, rows[0].ID, items[0].EngagementID)

	notified, err := s.db.NewSelect().Model((*models.UserNotification)(nil)).Count(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, notified, "comments older than the feed are stored without a notification")

	// A webhook delivered after Meta grants Advanced Access finds the same item.
	event := json.RawMessage(`{"field":"comments","value":{"id":"native-new","media":{"id":"native-reel"}}}`)
	require.NoError(t, s.RefreshFromWebhook(t.Context(), "a", event))
	count, err := s.db.NewSelect().Model((*models.EngagementItem)(nil)).Where("remote_id='native-new'").Count(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, count)
	notified, err = s.db.NewSelect().Model((*models.UserNotification)(nil)).Count(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, notified)
}

func TestUnchangedInstagramCommentCountsSkipCommentReads(t *testing.T) {
	s, provider := nativeCommentService(t, "instagram_basic instagram_manage_comments")
	now := time.Now().UTC()
	provider.media = []platform.InstagramMediaSummary{{ID: "native-reel", PublishedAt: now.Add(-time.Hour), CommentsCount: 1}}
	provider.comments["native-reel"] = []platform.Comment{instagramComment("first", "reader", "hello", now)}
	job := accountActivityReceipt(t, s)

	executeAccountActivity(t, s, job)
	executeAccountActivity(t, s, job)
	require.Equal(t, 2, provider.mediaLists)
	require.Equal(t, []string{"native-reel"}, provider.reads, "an unchanged count must not read comments again")

	provider.media[0].CommentsCount = 2
	provider.comments["native-reel"] = append(provider.comments["native-reel"], instagramComment("second", "other", "GRIND", now.Add(time.Minute)))
	executeAccountActivity(t, s, job)
	require.Equal(t, []string{"native-reel", "native-reel"}, provider.reads)
	count, err := s.db.NewSelect().Model((*models.EngagementItem)(nil)).Where("social_account_id='a' AND remote_id IN ('first','second')").Count(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

func TestInstagramCommentReadsAreBoundedPerJob(t *testing.T) {
	s, provider := nativeCommentService(t, "instagram_basic instagram_manage_comments")
	now := time.Now().UTC()
	for index := range nativeCommentReadsPerJob + 2 {
		id := fmt.Sprintf("media-%02d", index)
		provider.media = append(provider.media, platform.InstagramMediaSummary{ID: id, PublishedAt: now.Add(-time.Duration(index) * time.Hour), CommentsCount: 1})
		provider.comments[id] = []platform.Comment{instagramComment("comment-"+id, "reader", "hello", now)}
	}
	job := accountActivityReceipt(t, s)

	executeAccountActivity(t, s, job)
	require.Len(t, provider.reads, nativeCommentReadsPerJob)
	require.WithinDuration(t, time.Now().Add(nativeCommentCatchUp), nativeCommentState(t, s).NextSyncAt, 10*time.Second)

	executeAccountActivity(t, s, job)
	require.Equal(t, []string{"media-10", "media-11"}, provider.reads[nativeCommentReadsPerJob:])
	count, err := s.db.NewSelect().Model((*models.EngagementItem)(nil)).Where("social_account_id='a' AND remote_id LIKE 'comment-media-%'").Count(t.Context())
	require.NoError(t, err)
	require.Equal(t, nativeCommentReadsPerJob+2, count)
}

func TestInstagramCommentFeedRequiresMediaReadScope(t *testing.T) {
	s, provider := nativeCommentService(t, "instagram_manage_comments")
	_, err := s.RefreshWorkspace(t.Context(), Actor{UserID: "u"}, "w", true)
	require.NoError(t, err)
	require.Equal(t, "permission_required", nativeCommentState(t, s).Status)
	require.Zero(t, provider.mediaLists)
	count, err := s.db.NewSelect().Model((*models.Job)(nil)).Where("payload LIKE ?", `%"feed":"comments"%`).Count(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
}
