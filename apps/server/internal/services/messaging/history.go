package messaging

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/inboxchanges"
	"github.com/openpost/backend/internal/services/organizationguard"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/uptrace/bun"
)

func (s *Service) QueueHistory(ctx context.Context, actor Actor, workspaceID, conversationID string) error {
	if err := s.authorize(ctx, workspaceID, actor, workspaceaccess.LevelRead); err != nil {
		return err
	}
	var c models.Conversation
	if err := s.db.NewSelect().Model(&c).Where("id=? AND workspace_id=?", conversationID, workspaceID).Scan(ctx); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	var account models.SocialAccount
	if err := s.db.NewSelect().Model(&account).Where("id=? AND workspace_id=? AND is_active=?", c.SocialAccountID, workspaceID, true).Scan(ctx); err != nil {
		return err
	}
	if _, ok := s.provider(account).(platform.MessageHistoryAdapter); !ok {
		return nil
	}
	if !s.isMessagingEnabled(ctx, account.ID) {
		return errors.New("messaging is disabled for this account")
	}
	payload, _ := json.Marshal(subjectJob{ID: account.ID, ConversationID: c.ID})
	_, err := s.enqueue(ctx, workspaceID, JobTypeMessagesSync, string(payload), s.now())
	return err
}

func (s *Service) syncHistory(ctx context.Context, accountID, conversationID string) error {
	var account models.SocialAccount
	if err := s.db.NewSelect().Model(&account).Where("id=? AND is_active=?", accountID, true).Scan(ctx); err != nil {
		return err
	}
	if !s.isMessagingEnabled(ctx, account.ID) {
		return errors.New("messaging is disabled for this account")
	}
	provider, ok := s.provider(account).(platform.MessageHistoryAdapter)
	if !ok {
		return nil
	}
	var c models.Conversation
	if err := s.db.NewSelect().Model(&c).Where("id=? AND social_account_id=? AND workspace_id=?", conversationID, accountID, account.WorkspaceID).Scan(ctx); err != nil {
		return err
	}
	var checkpoint struct {
		Cursor   string
		Complete bool
	}
	err := s.db.NewRaw(`SELECT cursor,complete FROM message_history_checkpoints WHERE conversation_id=?`, c.ID).Scan(ctx, &checkpoint)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if checkpoint.Complete {
		return nil
	}
	token, err := s.messagingToken(ctx, account)
	if err != nil {
		return err
	}
	page, err := provider.FetchMessageHistory(ctx, token, platform.FetchMessageHistoryRequest{AccountID: account.AccountID, ConversationID: c.RemoteConversationID, Cursor: checkpoint.Cursor, Limit: 100})
	if err != nil {
		return err
	}
	return s.persistHistoryPage(ctx, account, c, page)
}

func (s *Service) persistHistoryPage(ctx context.Context, account models.SocialAccount, c models.Conversation, page platform.FetchMessageHistoryResult) error {
	return s.db.RunInTx(ctx, &sql.TxOptions{}, func(txCtx context.Context, tx bun.Tx) error {
		if err := organizationguard.LockWorkspace(txCtx, tx, account.WorkspaceID); err != nil {
			return err
		}
		if !s.isMessagingEnabledWithDB(txCtx, tx, account.ID) {
			return nil
		}
		active, err := tx.NewSelect().Model((*models.SocialAccount)(nil)).Where("id=? AND workspace_id=? AND is_active=?", account.ID, account.WorkspaceID, true).Exists(txCtx)
		if err != nil {
			return err
		}
		if !active {
			return ErrNotFound
		}
		deletions := make([]platform.DeletedProviderMessage, 0, len(page.DeletedMessages))
		for _, deleted := range page.DeletedMessages {
			// A history page can only delete messages in its requested conversation.
			deleted.ConversationID = c.RemoteConversationID
			deletions = append(deletions, deleted)
		}
		changes, err := s.applyDeletedMessages(txCtx, tx, account, deletions)
		if err != nil {
			return err
		}
		// History is not a new incoming event: retain its position and unread state.
		for _, remote := range page.Messages {
			if err := s.storeHistoryMessage(txCtx, tx, account, c, remote); err != nil {
				return err
			}
		}

		_, err = tx.NewRaw(`INSERT INTO message_history_checkpoints(conversation_id,cursor,complete) VALUES(?,?,?) ON CONFLICT(conversation_id) DO UPDATE SET cursor=EXCLUDED.cursor,complete=EXCLUDED.complete`, c.ID, page.NextCursor, page.NextCursor == "").Exec(txCtx)
		if err != nil {
			return err
		}
		changes = append(changes, inboxchanges.Change{Kind: "message", ConversationID: c.ID})
		return inboxchanges.Append(txCtx, tx, account.WorkspaceID, changes...)
	})
}

func (s *Service) historyAvailable(ctx context.Context, conversation models.Conversation) (bool, error) {
	var account models.SocialAccount
	if err := s.db.NewSelect().Model(&account).Where("id=? AND workspace_id=?", conversation.SocialAccountID, conversation.WorkspaceID).Scan(ctx); err != nil {
		return false, err
	}
	if _, ok := s.provider(account).(platform.MessageHistoryAdapter); !ok {
		return false, nil
	}
	var complete bool
	err := s.db.NewRaw(`SELECT complete FROM message_history_checkpoints WHERE conversation_id=?`, conversation.ID).Scan(ctx, &complete)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	return !complete, err
}
