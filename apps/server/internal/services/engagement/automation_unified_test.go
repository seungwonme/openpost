package engagement

import (
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPrivateReplyEligibilityScopesPerInstagramConnection(t *testing.T) {
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	s := &Service{now: func() time.Time { return now }}
	item := models.EngagementItem{Platform: "instagram", CanReply: true, AuthorRemoteID: "reader", RemoteCreatedAt: now.Add(-time.Hour)}
	for _, tc := range []struct {
		name, state, scopes, wantErr string
	}{
		{
			name:    "unified login with comment permission only",
			state:   `{"connection_type":"unified_login"}`,
			scopes:  "instagram_basic instagram_manage_comments",
			wantErr: "comment and message permissions",
		},
		{
			name:    "unified login without instagram_basic",
			state:   `{"connection_type":"unified_login"}`,
			scopes:  "instagram_manage_comments instagram_manage_messages",
			wantErr: "comment and message permissions",
		},
		{
			name:    "unified login with no recorded scopes is not treated as granted",
			state:   `{"connection_type":"unified_login"}`,
			scopes:  "",
			wantErr: "comment and message permissions",
		},
		{
			name:   "unified login with every documented permission",
			state:  `{"connection_type":"unified_login"}`,
			scopes: "instagram_basic instagram_manage_comments instagram_manage_messages",
		},
		{
			name:   "facebook login still needs only the comment permission",
			state:  `{"facebook_page_id":"1"}`,
			scopes: "instagram_manage_comments",
		},
		{
			name:    "facebook login without it",
			state:   `{"facebook_page_id":"1"}`,
			scopes:  "instagram_basic",
			wantErr: "comment permissions before",
		},
		{
			name:   "instagram login keeps its own scope",
			state:  `{"connection_type":"instagram_login"}`,
			scopes: "instagram_business_manage_comments",
		},
		{
			name:    "instagram login does not accept the Facebook scope",
			state:   `{"connection_type":"instagram_login"}`,
			scopes:  "instagram_manage_comments",
			wantErr: "comment permissions before",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := models.SocialAccount{Platform: "instagram", CapabilityState: tc.state, GrantedScopes: tc.scopes}
			err := s.privateReplyEligibility(item, account, "details")
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
