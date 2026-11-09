package messaging

import (
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/stretchr/testify/assert"
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

func (f automationFixture) seedSyncState(t *testing.T, status string, next time.Time) {
	t.Helper()
	_, err := f.db.NewInsert().Model(&models.MessagingSyncState{ID: stateID(f.account.ID), WorkspaceID: f.account.WorkspaceID, SocialAccountID: f.account.ID, Platform: f.account.Platform, Status: status, BackfillComplete: true, EmptyStreak: 12, NextSyncAt: next, LastAttemptedAt: f.now.Add(-6 * time.Hour), CreatedAt: *f.now, UpdatedAt: *f.now}).Exec(t.Context())
	require.NoError(t, err)
}

func (f automationFixture) nextSync(t *testing.T) time.Time {
	t.Helper()
	var state models.MessagingSyncState
	require.NoError(t, f.db.NewSelect().Model(&state).Where("id = ?", stateID(f.account.ID)).Scan(t.Context()))
	return state.NextSyncAt.UTC()
}

func (f automationFixture) queuedSyncs(t *testing.T) int {
	t.Helper()
	count, err := f.db.NewSelect().Model((*models.Job)(nil)).Where("type = ? AND status = 'pending'", JobTypeMessagesSync).Count(t.Context())
	require.NoError(t, err)
	return count
}

// Without webhooks only polling sees the reply a comment campaign waits for,
// so an open wait keeps a quiet account's collection within two minutes until
// its respond_by passes.
func TestOpenResponseWaitCollectsWithinTwoMinutes(t *testing.T) {
	f := newAutomationFixture(t)
	f.seedSyncState(t, "ok", time.Time{})
	in := f.commentSequenceRequest(t)
	require.Equal(t, SequenceWaitingResponse, f.advanceSequence(t, in).State)

	require.NoError(t, f.s.HandleJob(t.Context(), JobTypeMessagesSync, `{"id":"account"}`))
	assert.Equal(t, f.now.Add(2*time.Minute), f.nextSync(t), "an open wait caps the quiet backoff")
	assert.Equal(t, time.Minute, f.s.automationCadence(t.Context(), f.account, time.Minute), "a shorter cadence is never lengthened")

	*f.now = in.Comment.RespondBy.Add(time.Minute)
	require.NoError(t, f.s.HandleJob(t.Context(), JobTypeMessagesSync, `{"id":"account"}`))
	assert.Equal(t, f.now.Add(6*time.Hour), f.nextSync(t), "an expired wait returns the account to its backoff")
}

func TestStartingAResponseWaitPullsTheScheduledSyncForward(t *testing.T) {
	t.Run("a far sync moves within two minutes and queues once", func(t *testing.T) {
		f := newAutomationFixture(t)
		f.seedSyncState(t, "ok", f.now.Add(6*time.Hour))
		require.NoError(t, f.s.enqueueWaitingAccounts(t.Context()))
		require.Zero(t, f.queuedSyncs(t), "nothing waits yet")

		require.Equal(t, SequenceWaitingResponse, f.advanceSequence(t, f.commentSequenceRequest(t)).State)
		assert.Equal(t, f.now.Add(2*time.Minute), f.nextSync(t))
		require.NoError(t, f.s.enqueueWaitingAccounts(t.Context()))
		assert.Zero(t, f.queuedSyncs(t), "the sync is not due before its moved time")

		*f.now = f.now.Add(2 * time.Minute)
		require.NoError(t, f.s.enqueueWaitingAccounts(t.Context()))
		require.NoError(t, f.s.enqueueWaitingAccounts(t.Context()))
		assert.Equal(t, 1, f.queuedSyncs(t), "the collector loop queues one sync, not one per pass")
	})
	t.Run("a sooner sync is kept", func(t *testing.T) {
		f := newAutomationFixture(t)
		f.seedSyncState(t, "ok", f.now.Add(time.Minute))
		require.Equal(t, SequenceWaitingResponse, f.advanceSequence(t, f.commentSequenceRequest(t)).State)
		assert.Equal(t, f.now.Add(time.Minute), f.nextSync(t))
	})
	t.Run("a provider backoff is kept", func(t *testing.T) {
		f := newAutomationFixture(t)
		f.seedSyncState(t, "failed", f.now.Add(time.Hour))
		require.Equal(t, SequenceWaitingResponse, f.advanceSequence(t, f.commentSequenceRequest(t)).State)
		assert.Equal(t, f.now.Add(time.Hour), f.nextSync(t), "a rate limit or failure keeps its retry time")
		*f.now = f.now.Add(2 * time.Minute)
		require.NoError(t, f.s.enqueueWaitingAccounts(t.Context()))
		assert.Zero(t, f.queuedSyncs(t))
	})
}
