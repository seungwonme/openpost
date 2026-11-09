package messaging

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/organizationguard"
	"github.com/uptrace/bun"
)

type eventCheckpoint struct {
	Cursor          string
	BootstrapCursor string
	Initialized     bool
	HistoryCursor   string
	HistoryComplete bool
}

// RunCollector schedules durable account work. It does not hold a worker job
// open while a person reads a chat, and all devices share the same account job.
func (s *Service) RunCollector(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := s.enqueueActiveAccounts(ctx); err != nil && ctx.Err() == nil {
			log.Printf("schedule active inbox collection: %v", err)
		}
		if err := s.enqueueWaitingAccounts(ctx); err != nil && ctx.Err() == nil {
			log.Printf("schedule collection for waiting campaigns: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) enqueueActiveAccounts(ctx context.Context) error {
	now := s.now()
	if _, err := s.db.NewRaw(`DELETE FROM inbox_activity WHERE expires_at<=?`, now).Exec(ctx); err != nil {
		return err
	}
	var accounts []models.SocialAccount
	// next_poll_at survives job completion, process restarts, and a lost wake-up
	// while the coalesced account job was already processing.
	err := s.db.NewSelect().Model(&accounts).
		Join("LEFT JOIN message_event_checkpoints AS checkpoint ON checkpoint.social_account_id=social_account.id").
		Where("social_account.is_active=?", true).
		Where("(checkpoint.next_poll_at IS NULL OR checkpoint.next_poll_at<=?)", now).
		Where(`NOT EXISTS (SELECT 1 FROM messaging_sync_states AS state WHERE state.social_account_id=social_account.id AND state.status NOT IN ('ok','pending') AND state.next_sync_at>?)`, now).
		Where(`EXISTS (SELECT 1 FROM inbox_activity AS activity JOIN workspace_members AS member ON member.workspace_id=activity.workspace_id AND member.user_id=activity.user_id AND member.status='active' WHERE activity.workspace_id=social_account.workspace_id AND activity.expires_at>? AND activity.view IN ('chats','conversation') AND (activity.social_account_id='' OR activity.social_account_id=social_account.id))`, now).
		Limit(100).Scan(ctx)
	if err != nil {
		return err
	}
	for _, account := range accounts {
		if !s.isMessagingEnabled(ctx, account.ID) {
			continue
		}
		provider := s.provider(account)
		if provider == nil || !provider.MessagingSupport().Enabled {
			continue
		}
		payload, _ := json.Marshal(subjectJob{ID: account.ID})
		if _, err = s.enqueue(ctx, account.WorkspaceID, JobTypeMessagesSync, string(payload), now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) collectionCadence(ctx context.Context, account models.SocialAccount, fallback time.Duration) time.Duration {
	var active bool
	err := s.db.NewRaw(`SELECT EXISTS(SELECT 1 FROM inbox_activity WHERE workspace_id=? AND expires_at>? AND view IN ('chats','conversation') AND (social_account_id='' OR social_account_id=?))`, account.WorkspaceID, s.now(), account.ID).Scan(ctx, &active)
	if err != nil || !active {
		return fallback
	}
	if account.Platform == "bluesky" {
		var focused bool
		_ = s.db.NewRaw(`SELECT EXISTS(SELECT 1 FROM inbox_activity WHERE social_account_id=? AND view='conversation' AND expires_at>?)`, account.ID, s.now()).Scan(ctx, &focused)
		if focused {
			return 4 * time.Second
		}
		return 30 * time.Second
	}
	if account.Platform == "mastodon" {
		var streaming bool
		if err := s.db.NewRaw(`SELECT EXISTS(SELECT 1 FROM provider_inbox_transport_leases WHERE account_id=? AND expires_at>?)`, account.ID, s.now().UnixMilli()).Scan(ctx, &streaming); err == nil && streaming {
			return fallback
		}
	}
	// X's durable adapter quota is authoritative. Never speed up beyond its
	// published per-user read allowance. Webhook/stream providers use REST repair.
	if account.Platform == "x" {
		return 60 * time.Second
	}
	return time.Minute
}

func (s *Service) acquireCollection(ctx context.Context, accountID string) (string, bool, error) {
	owner := uuid.NewString()
	if _, err := s.db.NewRaw(`INSERT INTO message_event_checkpoints(social_account_id) VALUES(?) ON CONFLICT DO NOTHING`, accountID).Exec(ctx); err != nil {
		return "", false, err
	}
	result, err := s.db.NewRaw(`UPDATE message_event_checkpoints SET lease_owner=?,lease_until=? WHERE social_account_id=? AND (lease_until IS NULL OR lease_until<=?) AND NOT EXISTS (SELECT 1 FROM messaging_sync_states AS state WHERE state.social_account_id=message_event_checkpoints.social_account_id AND state.status NOT IN ('ok','pending') AND state.next_sync_at>?)`, owner, s.now().Add(2*time.Minute), accountID, s.now(), s.now()).Exec(ctx)
	if err != nil {
		return "", false, err
	}
	rows, err := result.RowsAffected()
	return owner, rows == 1, err
}
func (s *Service) finishCollection(ctx context.Context, accountID, owner string, delay time.Duration, failed bool) {
	failures := "0"
	if failed {
		failures = "failure_count+1"
	}
	proposed := s.now().Add(delay)
	// Foreground cadence may shorten healthy polling, never a stored failure or
	// permission deadline. Select its later value in the same atomic lease release.
	_, err := s.db.NewRaw(`UPDATE message_event_checkpoints SET lease_owner='',lease_until=NULL,next_poll_at=COALESCE((SELECT next_sync_at FROM messaging_sync_states WHERE social_account_id=? AND status NOT IN ('ok','pending') AND next_sync_at>? ORDER BY next_sync_at DESC LIMIT 1),?),failure_count=`+failures+` WHERE social_account_id=? AND lease_owner=?`, accountID, proposed, proposed, accountID, owner).Exec(ctx)
	if err != nil {
		log.Printf("save inbox collection schedule: %v", err)
	}
}

func providerRetryDelay(failure error, fallback time.Duration) time.Duration {
	var providerError *platform.HTTPError
	if errors.As(failure, &providerError) && providerError.RetryAfter > fallback {
		return providerError.RetryAfter
	}
	return fallback
}

func (s *Service) syncIncremental(ctx context.Context, account models.SocialAccount, provider Provider, incremental platform.IncrementalMessagingAdapter, token string) error {
	var checkpoint eventCheckpoint
	if err := s.db.NewRaw(`SELECT cursor,bootstrap_cursor,initialized,history_cursor,history_complete FROM message_event_checkpoints WHERE social_account_id=?`, account.ID).Scan(ctx, &checkpoint); err != nil {
		return err
	}
	if !checkpoint.Initialized {
		return s.bootstrapMessageEvents(ctx, account, provider, incremental, token, checkpoint)
	}

	for range 4 {
		events, err := incremental.FetchMessageEvents(ctx, token, platform.FetchMessageEventsRequest{AccountID: account.AccountID, Cursor: checkpoint.Cursor, Limit: 20})
		if err != nil {
			return err
		}
		if events.Bootstrap {
			return errors.New("provider unexpectedly reset the message event log")
		}
		if err = s.ApplyProviderEvents(ctx, account.ID, events); err != nil {
			return err
		}
		if events.NextCursor != "" {
			checkpoint.Cursor = events.NextCursor
		}
		if !events.HasMore {
			return nil
		}
	}
	return nil // Bounded batch; the committed cursor continues next collection.
}

func (s *Service) bootstrapMessageEvents(ctx context.Context, account models.SocialAccount, provider Provider, incremental platform.IncrementalMessagingAdapter, token string, checkpoint eventCheckpoint) error {
	if checkpoint.BootstrapCursor == "" {
		head, err := incremental.FetchMessageEvents(ctx, token, platform.FetchMessageEventsRequest{AccountID: account.AccountID, Limit: 20})
		if err != nil {
			return err
		}
		if !head.Bootstrap || head.NextCursor == "" {
			return platform.ErrMessageBootstrapCursorRequired
		}
		if _, err = s.db.NewRaw(`UPDATE message_event_checkpoints SET bootstrap_cursor=? WHERE social_account_id=? AND bootstrap_cursor=''`, head.NextCursor, account.ID).Exec(ctx); err != nil {
			return err
		}
		checkpoint.BootstrapCursor = head.NextCursor
	}
	snapshot, err := provider.FetchMessages(ctx, token, platform.FetchMessagesRequest{AccountID: account.AccountID, Cursor: checkpoint.HistoryCursor, Limit: 100})
	if err != nil {
		return err
	}
	return s.db.RunInTx(ctx, &sql.TxOptions{}, func(txCtx context.Context, tx bun.Tx) error {
		if err := organizationguard.LockWorkspace(txCtx, tx, account.WorkspaceID); err != nil {
			return err
		}
		// persist owner is transaction-local; checkpoint and inbox cannot diverge.
		if _, err := s.persistConversationsWithDB(txCtx, tx, account, snapshot.Conversations); err != nil {
			return err
		}
		complete := snapshot.NextCursor == ""
		if _, err = tx.NewRaw(`UPDATE message_event_checkpoints SET history_cursor=?,history_complete=?,initialized=?,cursor=? WHERE social_account_id=?`, snapshot.NextCursor, complete, complete, checkpoint.BootstrapCursor, account.ID).Exec(txCtx); err != nil {
			return err
		}
		return nil
	})
}
