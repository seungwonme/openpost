package messaging

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/uptrace/bun"
)

// messageWorkflowCadence is the longest background interval for an account an
// enabled message_received workflow watches. A quiet account otherwise backs
// off to six hours, and the workflow would answer that late.
const messageWorkflowCadence = 15 * time.Minute

// responseWaitCadence is the longest background interval while a comment
// campaign waits for someone's reply on the account. Without webhooks only
// polling sees a tapped "I followed", and the person is waiting for the link.
// One pass is one conversations read (two while history backfills), so this
// costs about 30 reads an hour per waiting account; provider failures keep
// their own backoff through messaging_sync_states.
const responseWaitCadence = 2 * time.Minute

// automationCadence keeps the quiet-account backoff from slowing message
// workflows. It never lengthens the proposed cadence.
func (s *Service) automationCadence(ctx context.Context, account models.SocialAccount, backoff time.Duration) time.Duration {
	if backoff > responseWaitCadence {
		if waiting, err := s.responseWaitOpen(ctx, s.db, account.ID); err == nil && waiting {
			return responseWaitCadence
		}
	}
	if backoff <= messageWorkflowCadence {
		return backoff
	}
	watched, err := s.messageWorkflowWatches(ctx, account)
	if err != nil || !watched {
		return backoff
	}
	return messageWorkflowCadence
}

func (s *Service) messageWorkflowWatches(ctx context.Context, account models.SocialAccount) (bool, error) {
	var published []string
	if err := s.db.NewSelect().Table("workflows").Column("published_json").
		Where("workspace_id = ? AND enabled = ?", account.WorkspaceID, true).Scan(ctx, &published); err != nil {
		return false, err
	}
	for _, raw := range published {
		var definition struct {
			Source struct {
				Kind       string   `json:"kind"`
				AccountIDs []string `json:"account_ids"`
			} `json:"source"`
		}
		if json.Unmarshal([]byte(raw), &definition) != nil {
			continue
		}
		if definition.Source.Kind == "message_received" && slices.Contains(definition.Source.AccountIDs, account.ID) {
			return true, nil
		}
	}
	return false, nil
}

// responseWaitOpen reports a comment campaign on the account still waiting for
// a reply. A wait past respond_by no longer speeds anything up.
func (s *Service) responseWaitOpen(ctx context.Context, db bun.IDB, accountID string) (bool, error) {
	return db.NewSelect().Table("message_sequence_waits").Where("account_id = ? AND respond_by > ?", accountID, s.now()).Exists(ctx)
}

// expediteWaitingCollection pulls a healthy account's scheduled sync forward to
// within responseWaitCadence when a wait starts or moves. It never delays a sync
// and never shortens a failure or rate-limit backoff.
func (s *Service) expediteWaitingCollection(ctx context.Context, db bun.IDB, accountID string) error {
	at := s.now().Add(responseWaitCadence)
	_, err := db.NewUpdate().Table("messaging_sync_states").Set("next_sync_at = ?", at).
		Where("social_account_id = ? AND status = 'ok' AND next_sync_at > ?", accountID, at).Exec(ctx)
	return err
}

// enqueueWaitingAccounts queues the messages sync of every account with an open
// wait once its schedule is due. The sweep only runs every five minutes; the
// job's active-subject index keeps this to one queued sync per account.
func (s *Service) enqueueWaitingAccounts(ctx context.Context) error {
	now := s.now()
	var accounts []models.SocialAccount
	err := s.db.NewSelect().Model(&accounts).Where("social_account.is_active = ?", true).
		Where("EXISTS (SELECT 1 FROM message_sequence_waits AS w WHERE w.account_id = social_account.id AND w.respond_by > ?)", now).
		Where("NOT EXISTS (SELECT 1 FROM messaging_sync_states AS state WHERE state.social_account_id = social_account.id AND state.next_sync_at > ?)", now).
		Limit(100).Scan(ctx)
	if err != nil {
		return err
	}
	for _, account := range accounts {
		provider := s.provider(account)
		if provider == nil || !provider.MessagingSupport().Enabled || !s.isMessagingEnabled(ctx, account.ID) {
			continue
		}
		payload, _ := json.Marshal(subjectJob{ID: account.ID})
		if _, err := s.enqueue(ctx, account.WorkspaceID, JobTypeMessagesSync, string(payload), now); err != nil {
			return err
		}
	}
	return nil
}
