package engagement

import (
	"context"
	"fmt"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/providerwrite"
)

// privateQuickReplies keeps quick replies on private replies only: a public
// comment reply cannot carry them.
func privateQuickReplies(channel string, quickReplies []platform.QuickReply) []platform.QuickReply {
	if channel != "private" {
		return nil
	}
	return quickReplies
}

// engagementActionFingerprint keeps the historical fingerprint for every
// action without quick replies, so writes fenced before this change stay
// idempotent.
func engagementActionFingerprint(item *models.EngagementItem, input engagementActionJob) (string, error) {
	fields := map[string]string{"item_id": item.ID, "remote_id": item.RemoteID, "action": input.Action, "message": input.Message}
	for index, reply := range input.QuickReplies {
		fields[fmt.Sprintf("quick_reply_%d", index)] = reply.Title + "\x00" + reply.Payload
	}
	return providerwrite.Fingerprint("engagement-action-v1", fields)
}

// sendPrivateQuickReply never falls back to a plain private reply: a person
// would otherwise get a message asking them to tap a button that is not there.
func sendPrivateQuickReply(ctx context.Context, commenter platform.CommentAdapter, token string, account models.SocialAccount, item *models.EngagementItem, input engagementActionJob) (platform.PublishResult, error) {
	adapter, ok := commenter.(platform.PrivateCommentQuickReplyAdapter)
	if !ok {
		return platform.PublishResult{}, fmt.Errorf("this provider does not support quick replies on private comment replies")
	}
	id, err := adapter.ReplyPrivatelyToCommentWithQuickReplies(ctx, token, account.AccountID, item.RemoteID, input.Message, input.QuickReplies)
	if err != nil {
		return platform.PublishResult{}, err
	}
	return platform.AcceptedPublishResult(id), nil
}
