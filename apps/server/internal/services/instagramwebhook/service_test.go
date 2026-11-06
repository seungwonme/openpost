package instagramwebhook

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/openpost/backend/internal/database"
	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/accountfeatures"
	"github.com/openpost/backend/internal/services/workspaceaccess"
)

// fakeInstagram stands in for the Meta edge. HTTP request shape per login
// mode is asserted in the platform package; this fake records what the
// service hands to it and returns scripted outcomes.
type fakeInstagram struct {
	platform.Adapter
	inputs []platform.InstagramWebhookSubscription
	errs   []error
}

func (f *fakeInstagram) MessagingSupport() platform.MessagingSupport {
	return platform.MessagingSupport{Enabled: true}
}
func (*fakeInstagram) FetchMessages(context.Context, string, platform.FetchMessagesRequest) (platform.FetchMessagesResult, error) {
	return platform.FetchMessagesResult{}, nil
}
func (*fakeInstagram) SendMessage(context.Context, string, platform.SendMessageRequest) (platform.SendMessageResult, error) {
	return platform.SendMessageResult{}, nil
}
func (f *fakeInstagram) SubscribeInstagramWebhooks(_ context.Context, in platform.InstagramWebhookSubscription) error {
	f.inputs = append(f.inputs, in)
	if len(f.errs) == 0 {
		return nil
	}
	err := f.errs[0]
	f.errs = f.errs[1:]
	return err
}

type tokenSource struct{ token string }

func (s *tokenSource) GetValidAccessToken(context.Context, string) (string, error) {
	return s.token, nil
}

func testDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := database.InitDB("file:" + t.TempDir() + "/webhook.db?mode=rwc")
	require.NoError(t, err)
	require.NoError(t, database.CreateSchema(db))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func seedAccount(t *testing.T, db *bun.DB, id, platformName, capabilityState string) {
	t.Helper()
	ctx := t.Context()
	_, _ = db.NewInsert().Model(&models.User{ID: "u1", Email: "u1@example.test", PasswordHash: "x"}).On("CONFLICT DO NOTHING").Exec(ctx)
	_, _ = db.NewInsert().Model(&models.Workspace{ID: "w1", Name: "Workspace"}).On("CONFLICT DO NOTHING").Exec(ctx)
	_, _ = db.NewInsert().Model(&models.WorkspaceMember{WorkspaceID: "w1", UserID: "u1", Role: models.WorkspaceRoleEditor, Status: models.WorkspaceMemberStatusActive}).On("CONFLICT DO NOTHING").Exec(ctx)
	_, err := db.NewInsert().Model(&models.SocialAccount{
		ID: id, WorkspaceID: "w1", Slug: id, Platform: platformName, AccountID: "17841400000000123",
		AccessTokenEnc: []byte("x"), CapabilityState: capabilityState, IsActive: true,
	}).Exec(ctx)
	require.NoError(t, err)
}

func readiness(t *testing.T, db *bun.DB) []models.EngagementSyncState {
	t.Helper()
	states, err := EngagementStates(t.Context(), db, "w1")
	require.NoError(t, err)
	return states
}

func messageReadiness(t *testing.T, db *bun.DB) []models.MessagingSyncState {
	t.Helper()
	states, err := MessagingStates(t.Context(), db, "w1")
	require.NoError(t, err)
	return states
}

func TestEnableFeatureSubscribesAndFailureNeverBlocksTheToggle(t *testing.T) {
	db := testDB(t)
	seedAccount(t, db, "ig1", "instagram", `{"facebook_page_id":"104200000000456"}`)
	adapter := &fakeInstagram{errs: []error{&platform.HTTPError{StatusCode: 403, Code: "10"}}}
	svc := NewService(db, &tokenSource{token: "page-token-1"}, map[string]platform.Adapter{"instagram": adapter})
	features := accountfeatures.NewService(db, map[string]platform.Adapter{"instagram": adapter}, nil)
	features.SetWebhookSubscriber(svc)
	actor := workspaceaccess.ActorFacts{UserID: "u1"}

	// First attempt: Meta denies. The toggle still saves and the failure is
	// visible through the engagement sync states with an actionable message.
	_, err := features.BatchSave(t.Context(), "w1", actor, []accountfeatures.ChoiceInput{{AccountID: "ig1", Feature: accountfeatures.FeatureMessaging, Enabled: true}})
	require.NoError(t, err)
	enabled, err := features.IsEffectiveEnabled(t.Context(), "ig1", accountfeatures.FeatureMessaging)
	require.NoError(t, err)
	require.True(t, enabled)
	require.Equal(t, []platform.InstagramWebhookSubscription{{AccessToken: "page-token-1", AccountID: "17841400000000123", PageID: "104200000000456"}}, adapter.inputs)
	// Direct messages only: the failure is a Messages row, never Comments,
	// and it is not a successful collection.
	require.Empty(t, readiness(t, db))
	messages := messageReadiness(t, db)
	require.Len(t, messages, 1)
	require.Equal(t, "ig1", messages[0].SocialAccountID)
	require.Equal(t, "permission_required", messages[0].Status)
	require.Equal(t, platform.InstagramWebhookPermissionDenied, messages[0].ErrorCode)
	require.Contains(t, messages[0].ErrorMessage, "Advanced Access")
	require.NotContains(t, messages[0].ErrorMessage, "page-token-1")
	require.True(t, messages[0].LastSuccessAt.IsZero(), "a failed subscription is not a successful sync")

	// Turning Direct messages off and on retries and succeeds, so no row is
	// emitted at all (a success is not a sync state).
	_, err = features.BatchSave(t.Context(), "w1", actor, []accountfeatures.ChoiceInput{{AccountID: "ig1", Feature: accountfeatures.FeatureMessaging, Enabled: false}})
	require.NoError(t, err)
	_, err = features.BatchSave(t.Context(), "w1", actor, []accountfeatures.ChoiceInput{{AccountID: "ig1", Feature: accountfeatures.FeatureMessaging, Enabled: true}})
	require.NoError(t, err)
	require.Len(t, adapter.inputs, 2)
	require.Empty(t, readiness(t, db))
	require.Empty(t, messageReadiness(t, db))
	var row models.InstagramWebhookSubscription
	require.NoError(t, db.NewSelect().Model(&row).Where("social_account_id = ?", "ig1").Scan(t.Context()))
	require.Equal(t, StatusSubscribed, row.Status)
}

func TestFailureShowsUnderEachEnabledFeatureOnly(t *testing.T) {
	db := testDB(t)
	seedAccount(t, db, "ig1", "instagram", `{}`)
	adapter := &fakeInstagram{errs: []error{&platform.HTTPError{StatusCode: 503}}}
	svc := NewService(db, &tokenSource{token: "t"}, map[string]platform.Adapter{"instagram": adapter})
	require.Error(t, svc.Ensure(t.Context(), "ig1"))
	now := time.Now().UTC()
	for _, feature := range []string{accountfeatures.FeatureEngagement, accountfeatures.FeatureMessaging} {
		_, err := db.NewInsert().Model(&models.AccountFeature{SocialAccountID: "ig1", WorkspaceID: "w1", Feature: feature, Enabled: true, Source: "test", DecidedAt: now, CreatedAt: now, UpdatedAt: now}).Exec(t.Context())
		require.NoError(t, err)
		if feature == accountfeatures.FeatureEngagement {
			require.Len(t, readiness(t, db), 1)
			require.Empty(t, messageReadiness(t, db))
		} else {
			require.Len(t, readiness(t, db), 1)
			require.Len(t, messageReadiness(t, db), 1)
		}
	}
	comments := readiness(t, db)[0]
	require.Equal(t, "temporarily_unavailable", comments.Status)
	require.True(t, comments.LastSuccessAt.IsZero())
}

func TestSuccessfulSubscriptionEmitsNoSyncState(t *testing.T) {
	db := testDB(t)
	seedAccount(t, db, "ig1", "instagram", `{}`)
	adapter := &fakeInstagram{}
	svc := NewService(db, &tokenSource{token: "t"}, map[string]platform.Adapter{"instagram": adapter})
	now := time.Now().UTC()
	for _, feature := range []string{accountfeatures.FeatureEngagement, accountfeatures.FeatureMessaging} {
		_, err := db.NewInsert().Model(&models.AccountFeature{SocialAccountID: "ig1", WorkspaceID: "w1", Feature: feature, Enabled: true, Source: "test", DecidedAt: now, CreatedAt: now, UpdatedAt: now}).Exec(t.Context())
		require.NoError(t, err)
	}
	require.NoError(t, svc.Ensure(t.Context(), "ig1"))
	require.Empty(t, readiness(t, db), "mobile takes the newest last_success_at across sync states")
	require.Empty(t, messageReadiness(t, db))
}

func TestDisablingOrOtherPlatformsDoNotSubscribe(t *testing.T) {
	db := testDB(t)
	seedAccount(t, db, "ig1", "instagram", `{"facebook_page_id":"1"}`)
	seedAccount(t, db, "x1", "x", `{}`)
	adapter := &fakeInstagram{}
	svc := NewService(db, &tokenSource{token: "t"}, map[string]platform.Adapter{"instagram": adapter, "x": adapter})
	features := accountfeatures.NewService(db, map[string]platform.Adapter{"instagram": adapter, "x": adapter}, nil)
	features.SetWebhookSubscriber(svc)
	actor := workspaceaccess.ActorFacts{UserID: "u1"}

	_, err := features.BatchSave(t.Context(), "w1", actor, []accountfeatures.ChoiceInput{
		{AccountID: "ig1", Feature: accountfeatures.FeatureMessaging, Enabled: false},
		{AccountID: "x1", Feature: accountfeatures.FeatureMessaging, Enabled: true},
	})
	require.NoError(t, err)
	require.Empty(t, adapter.inputs)
	require.Empty(t, readiness(t, db))
}

func TestReconnectResubscribesWithTheNewTokenOnlyWhenAFeatureIsOn(t *testing.T) {
	db := testDB(t)
	seedAccount(t, db, "ig1", "instagram", `{"connection_type":"instagram_login"}`)
	adapter := &fakeInstagram{}
	tokens := &tokenSource{token: "first-token"}
	svc := NewService(db, tokens, map[string]platform.Adapter{"instagram:instagram_login": adapter})

	// Reconnecting an account with no webhook feature on is not a Meta call.
	require.NoError(t, svc.EnsureIfEnabled(t.Context(), "ig1"))
	require.Empty(t, adapter.inputs)

	now := time.Now().UTC()
	_, err := db.NewInsert().Model(&models.AccountFeature{SocialAccountID: "ig1", WorkspaceID: "w1", Feature: accountfeatures.FeatureEngagement, Enabled: true, Source: "test", DecidedAt: now, CreatedAt: now, UpdatedAt: now}).Exec(t.Context())
	require.NoError(t, err)
	require.NoError(t, svc.EnsureIfEnabled(t.Context(), "ig1"))
	tokens.token = "reconnected-token"
	require.NoError(t, svc.EnsureIfEnabled(t.Context(), "ig1"))

	require.Len(t, adapter.inputs, 2)
	require.Equal(t, "first-token", adapter.inputs[0].AccessToken)
	require.Equal(t, "reconnected-token", adapter.inputs[1].AccessToken)
	require.Equal(t, "17841400000000123", adapter.inputs[1].AccountID)
	var rows int
	rows, err = db.NewSelect().Model((*models.InstagramWebhookSubscription)(nil)).Count(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, rows, "repeat subscription updates one row")
}

func TestFailureReportingIsActionableAndTokenFree(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status string
		code   string
	}{
		{"expired token", &platform.HTTPError{StatusCode: 400, Code: "190"}, "permission_required", platform.InstagramWebhookAuthRequired},
		{"meta outage", &platform.HTTPError{StatusCode: 503}, "temporarily_unavailable", platform.InstagramWebhookUnavailable},
		{"throttled", &platform.HTTPError{StatusCode: 429}, "rate_limited", platform.InstagramWebhookRateLimited},
		{"rejected", &platform.HTTPError{StatusCode: 400, Code: "100"}, "error", platform.InstagramWebhookRejected},
		{"no linked page", platform.ErrInstagramWebhookUnsupported, "error", platform.InstagramWebhookUnsupportedConnection},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testDB(t)
			seedAccount(t, db, "ig1", "instagram", `{}`)
			adapter := &fakeInstagram{errs: []error{tc.err}}
			svc := NewService(db, &tokenSource{token: "secret-token"}, map[string]platform.Adapter{"instagram": adapter})
			err := svc.Ensure(t.Context(), "ig1")
			require.Error(t, err)
			var row models.InstagramWebhookSubscription
			require.NoError(t, db.NewSelect().Model(&row).Where("social_account_id = ?", "ig1").Scan(t.Context()))
			require.Equal(t, StatusFailed, row.Status)
			require.Equal(t, tc.code, row.ErrorCode)
			require.NotEmpty(t, row.ErrorMessage)
			require.False(t, strings.Contains(row.ErrorMessage+row.ErrorCode, "secret-token"))
			require.True(t, row.LastSuccessAt.IsZero())
		})
	}
}

func TestReadinessHidesStaleFailureOnceFeaturesAreOff(t *testing.T) {
	db := testDB(t)
	seedAccount(t, db, "ig1", "instagram", `{}`)
	adapter := &fakeInstagram{errs: []error{errors.New("boom")}}
	svc := NewService(db, &tokenSource{token: "t"}, map[string]platform.Adapter{"instagram": adapter})
	require.Error(t, svc.Ensure(t.Context(), "ig1"))
	require.Empty(t, readiness(t, db), "no subscribing feature is on")
}

func TestReconnectHookSubscribesOnlyInstagramAccounts(t *testing.T) {
	db := testDB(t)
	seedAccount(t, db, "ig1", "instagram", `{"facebook_page_id":"1"}`)
	adapter := &fakeInstagram{}
	svc := NewService(db, &tokenSource{token: "t"}, map[string]platform.Adapter{"instagram": adapter})
	now := time.Now().UTC()
	_, err := db.NewInsert().Model(&models.AccountFeature{SocialAccountID: "ig1", WorkspaceID: "w1", Feature: accountfeatures.FeatureMessaging, Enabled: true, Source: "test", DecidedAt: now, CreatedAt: now, UpdatedAt: now}).Exec(t.Context())
	require.NoError(t, err)
	ReconnectHook(svc)(t.Context(), []*models.SocialAccount{{ID: "x1", Platform: "x"}, {ID: "ig1", Platform: "instagram"}})
	require.Eventually(t, func() bool {
		n, err := db.NewSelect().Model((*models.InstagramWebhookSubscription)(nil)).Where("status = ?", StatusSubscribed).Count(t.Context())
		return err == nil && n == 1
	}, 3*time.Second, 10*time.Millisecond)
	require.Len(t, adapter.inputs, 1)
}
