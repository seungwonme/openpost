package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
)

const (
	automationRefreshMessages = 25
	automationRefreshRetry    = time.Minute
	// A provider outage holds a queued message this long before it stops, so a
	// message never reaches someone long after the moment it was written for.
	automationRefreshBound = 10 * time.Minute

	stopReasonRefresh = "OpenPost could not confirm this conversation with Instagram before sending, so this message was not sent."
)

// prepareAutomatedSend reads the conversation from the provider so stops are
// evaluated on what happened, not on what webhooks and polling have stored. A
// person's "stop" can be minutes old in the provider before it reaches OpenPost.
// Nothing automated is sent on stale data: anything that prevents the read, whether
// the account, the token or the provider, holds the message.
func (s *Service) prepareAutomatedSend(ctx context.Context, message models.DirectMessage, conversation models.Conversation) (bool, error) {
	hold, err := s.sendAutomationHold(ctx, message, conversation)
	if err != nil || hold {
		return hold, err
	}
	_, owned, err := s.automationOwnerOfMessage(ctx, s.db, message.ID)
	if err != nil || !owned {
		return false, err
	}
	if err := s.readConversationBeforeSend(ctx, conversation); err != nil {
		log.Printf("refresh conversation before automated send: %v", err)
		return true, s.holdAutomatedSend(ctx, message, conversation)
	}
	return s.sendAutomationHold(ctx, message, conversation)
}

func (s *Service) readConversationBeforeSend(ctx context.Context, conversation models.Conversation) error {
	var account models.SocialAccount
	if err := s.db.NewSelect().Model(&account).Where("id = ? AND workspace_id = ? AND is_active = ?", conversation.SocialAccountID, conversation.WorkspaceID, true).Scan(ctx); err != nil {
		return err
	}
	provider := s.provider(account)
	if provider == nil {
		return errors.New("this account has no messaging provider")
	}
	token, err := s.messagingToken(ctx, account)
	if err != nil {
		return err
	}
	return s.refreshConversation(ctx, conversation, account, provider, token)
}

func (s *Service) refreshConversation(ctx context.Context, conversation models.Conversation, account models.SocialAccount, provider Provider, token string) error {
	history, ok := provider.(platform.MessageHistoryAdapter)
	if !ok {
		return errors.New("this provider cannot read a single conversation")
	}
	page, err := history.FetchMessageHistory(ctx, token, platform.FetchMessageHistoryRequest{AccountID: account.AccountID, ConversationID: conversation.RemoteConversationID, Limit: automationRefreshMessages})
	if err != nil {
		return err
	}
	deleted := make([]platform.DeletedProviderMessage, 0, len(page.DeletedMessages))
	for _, message := range page.DeletedMessages {
		message.ConversationID = conversation.RemoteConversationID
		deleted = append(deleted, message)
	}
	remote := platform.ProviderConversation{ID: conversation.RemoteConversationID, CounterpartRemoteID: conversation.CounterpartRemoteID, Messages: page.Messages}
	window := provider.MessagingSupport().ReplyWindow
	for _, message := range page.Messages {
		if message.RemoteCreatedAt.After(remote.LastMessageAt) {
			remote.LastMessageAt, remote.LastMessagePreview, remote.LastRemoteMessageID = message.RemoteCreatedAt, message.Body, message.ID
		}
		if message.Direction == "inbound" && window > 0 && message.RemoteCreatedAt.Add(window).After(remote.ReplyWindowExpiresAt) {
			remote.ReplyWindowExpiresAt = message.RemoteCreatedAt.Add(window)
		}
	}
	return s.ApplyProviderEvents(ctx, account.ID, platform.FetchMessageEventsResult{Conversations: []platform.ProviderConversation{remote}, DeletedMessages: deleted})
}

func (s *Service) holdAutomatedSend(ctx context.Context, message models.DirectMessage, conversation models.Conversation) error {
	now := s.now()
	if now.Sub(message.CreatedAt) >= automationRefreshBound {
		return s.stopAutomatedMessage(ctx, message, conversation, &automationSendStopped{reason: stopReasonRefresh})
	}
	// One active job per payload is a queue rule, and this job is still processing,
	// so the retry carries its time. The send decodes only the message ID.
	retryAt := now.Add(automationRefreshRetry)
	payload, err := json.Marshal(struct {
		ID      string `json:"id"`
		RetryAt string `json:"retry_at"`
	}{ID: message.ID, RetryAt: retryAt.Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	inserted, err := s.enqueue(ctx, message.WorkspaceID, JobTypeMessageSend, string(payload), retryAt)
	if err == nil && !inserted {
		return errors.New("could not schedule the automated send retry")
	}
	return err
}
