package messaging

import (
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/stretchr/testify/require"
)

// A quiet account backs off to six hours. An enabled message workflow watching
// the account keeps it at the 15-minute base cadence instead.
func TestMessageWorkflowKeepsQuietAccountOnBaseCadence(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		for _, tc := range []struct {
			name      string
			kind      string
			accountID string
			enabled   bool
			want      time.Duration
		}{
			{name: "no workflow", want: 6 * time.Hour},
			{name: "enabled message workflow", kind: "message_received", accountID: "account", enabled: true, want: 15 * time.Minute},
			{name: "disabled message workflow", kind: "message_received", accountID: "account", want: 6 * time.Hour},
			{name: "message workflow for another account", kind: "message_received", accountID: "other", enabled: true, want: 6 * time.Hour},
			{name: "comment workflow", kind: "comment_received", accountID: "account", enabled: true, want: 6 * time.Hour},
		} {
			t.Run(backend+"/"+tc.name, func(t *testing.T) {
				s, db, account, actor := messagingBackendFixture(t, backend)
				now := time.Now().UTC().Truncate(time.Second)
				s.now = func() time.Time { return now }
				fetches := 0
				s.SetProvider("bluesky", testProvider{fetch: func() (platform.FetchMessagesResult, error) {
					fetches++
					return platform.FetchMessagesResult{}, nil
				}})
				_, err := db.NewInsert().Model(&models.MessagingSyncState{ID: stateID(account.ID), WorkspaceID: account.WorkspaceID, SocialAccountID: account.ID, Platform: account.Platform, Status: "ok", BackfillComplete: true, EmptyStreak: 12, LastAttemptedAt: now.Add(-6 * time.Hour), CreatedAt: now, UpdatedAt: now}).Exec(t.Context())
				require.NoError(t, err)
				if tc.kind != "" {
					published := `{"schema":1,"source":{"kind":"` + tc.kind + `","account_ids":["` + tc.accountID + `"],"keywords":["price"],"daily_limit":20},"steps":[]}`
					_, err = db.NewRaw(`INSERT INTO workflows(id,workspace_id,name,draft_json,published_json,authority_json,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "workflow", account.WorkspaceID, "Replies", "{}", published, "{}", tc.enabled, now, now).Exec(t.Context())
					require.NoError(t, err)
				}

				require.NoError(t, s.HandleJob(t.Context(), JobTypeMessagesSync, `{"id":"account"}`))

				require.Equal(t, 1, fetches)
				states, err := s.ListSyncStates(t.Context(), actor, "workspace")
				require.NoError(t, err)
				require.Len(t, states, 1)
				require.Equal(t, 13, states[0].EmptyStreak, "the quiet streak still grows")
				require.True(t, now.Add(tc.want).Equal(states[0].NextSyncAt), "next=%s, want %s", states[0].NextSyncAt, now.Add(tc.want))
			})
		}
	}
}
