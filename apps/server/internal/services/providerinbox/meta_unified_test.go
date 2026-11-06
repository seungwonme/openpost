package providerinbox

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/stretchr/testify/require"
)

const (
	unifiedCommentBody = `{"object":"instagram","entry":[{"id":"ig-self","changes":[{"field":"comments","value":{"id":"c1","text":"hi"}}]}]}`
)

// unifiedInboxApp is the Meta app of a config_id configuration. The app entry
// stays in oauth mode, so AppConfigMergeKey gives the callback ID.
func unifiedInboxApp(t *testing.T, s *Service, capabilityState, scopes string) MetaApp {
	t.Helper()
	_, err := s.db.NewUpdate().Model((*models.OAuthGrant)(nil)).
		Set("provider_project_id = ?, granted_scopes = ?", "meta-app", scopes).Where("id = ?", "grant").Exec(t.Context())
	require.NoError(t, err)
	_, err = s.db.NewUpdate().Model((*models.SocialAccount)(nil)).
		Set("capability_state_json = ?, granted_scopes = ?", capabilityState, scopes).Where("id = ?", "account").Exec(t.Context())
	require.NoError(t, err)
	merge := platform.AppConfigMergeKey(platform.AppConfig{Provider: "instagram", ClientID: "meta-app", ConfigID: "config-123"})
	require.Equal(t, "instagram", merge)
	return MetaApp{ID: merge + ":meta-app", ClientID: "meta-app", Platform: "instagram", ConnectionMode: platform.ConnectionModeOAuth, AppSecret: "app-secret", VerifyToken: "verify-secret"}
}

func deliverMeta(t *testing.T, s *Service, app MetaApp, body string) []receipt {
	t.Helper()
	response := httptest.NewRecorder()
	s.MetaHandler(app).ServeHTTP(response, signedRequest(body, app.AppSecret))
	require.Equal(t, http.StatusOK, response.Code)
	var rows []receipt
	require.NoError(t, s.db.NewSelect().Model(&rows).Scan(t.Context()))
	return rows
}

func TestMetaIntakeRoutesUnifiedLoginAccountsWithoutAPage(t *testing.T) {
	const unifiedState = `{"connection_type":"unified_login","instagram_account_type":"professional"}`

	t.Run("a message resolves by Instagram account ID without Page permissions", func(t *testing.T) {
		s, _ := inboxTestService(t)
		app := unifiedInboxApp(t, s, unifiedState, "instagram_basic instagram_manage_messages")
		rows := deliverMeta(t, s, app, inboundMetaBody)
		require.Len(t, rows, 1)
		require.Equal(t, "account", rows[0].AccountID)
		require.Equal(t, "grant", rows[0].OAuthGrantID)
		require.Equal(t, "message", rows[0].Kind)
		require.Equal(t, "instagram:meta-app", rows[0].ProviderAppID)
	})

	t.Run("a message is not stored without messaging permission, a comment is", func(t *testing.T) {
		s, _ := inboxTestService(t)
		app := unifiedInboxApp(t, s, unifiedState, "instagram_basic instagram_manage_comments")
		require.Empty(t, deliverMeta(t, s, app, inboundMetaBody), "scopes are checked, never assumed")
		rows := deliverMeta(t, s, app, unifiedCommentBody)
		require.Len(t, rows, 1)
		require.Equal(t, "engagement", rows[0].Kind)
	})

	t.Run("a direct-login callback never receives the account", func(t *testing.T) {
		s, direct := inboxTestService(t)
		app := unifiedInboxApp(t, s, unifiedState, "instagram_basic instagram_manage_messages")
		direct.ClientID = app.ClientID
		require.Empty(t, deliverMeta(t, s, direct, inboundMetaBody))
	})

	t.Run("a Facebook Login account still needs pages_manage_metadata", func(t *testing.T) {
		s, _ := inboxTestService(t)
		app := unifiedInboxApp(t, s, `{"facebook_page_id":"104200000000456"}`, "instagram_basic instagram_manage_messages")
		require.Empty(t, deliverMeta(t, s, app, inboundMetaBody))

		s, _ = inboxTestService(t)
		app = unifiedInboxApp(t, s, `{"facebook_page_id":"104200000000456"}`, "instagram_basic instagram_manage_messages pages_manage_metadata")
		require.Len(t, deliverMeta(t, s, app, inboundMetaBody), 1)
	})
}
