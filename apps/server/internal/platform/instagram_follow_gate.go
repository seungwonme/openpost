package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// MessageButton is one web_url button of a Meta button template. A button
// opens its URL in Instagram's in-app browser and needs no webhook.
type MessageButton struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// QuickReply is one plain-text quick reply. Tapping it posts its title to the
// conversation as the person's own message, which polling stores like any
// other message; a webhook also carries the payload.
type QuickReply struct {
	Title   string `json:"title"`
	Payload string `json:"payload"`
}

const (
	// FollowCheckQuickReplyPayload marks the quick reply a follow check asks
	// the person to tap.
	FollowCheckQuickReplyPayload = "OPENPOST_FOLLOW_CHECK"
	// QuickReplyTitleLimit is where Instagram truncates a quick reply title.
	QuickReplyTitleLimit = 20
	// MessageButtonTextLimit is the template text limit, in characters.
	MessageButtonTextLimit = 640
	// MaxMessageButtons is how many buttons one template can carry.
	MaxMessageButtons = 3
)

// metaOutgoingMessage is the `message` object of a Send API request: plain
// text, or a button template whose text appears above its buttons.
func metaOutgoingMessage(input SendMessageRequest) map[string]any {
	text := strings.TrimSpace(input.Body)
	if len(input.Buttons) == 0 {
		return metaTextMessage(text, input.QuickReplies)
	}
	buttons := make([]map[string]string, 0, len(input.Buttons))
	for _, button := range input.Buttons {
		buttons = append(buttons, map[string]string{"type": "web_url", "url": button.URL, "title": button.Title})
	}
	return map[string]any{"attachment": map[string]any{
		"type":    "template",
		"payload": map[string]any{"template_type": "button", "text": text, "buttons": buttons},
	}}
}

// metaTextMessage is a text message, with quick replies when there are any.
func metaTextMessage(text string, quickReplies []QuickReply) map[string]any {
	message := map[string]any{"text": text}
	if len(quickReplies) > 0 {
		replies := make([]map[string]string, 0, len(quickReplies))
		for _, reply := range quickReplies {
			replies = append(replies, map[string]string{"content_type": "text", "title": reply.Title, "payload": reply.Payload})
		}
		message["quick_replies"] = replies
	}
	return message
}

// PrivateCommentQuickReplyAdapter sends a private reply with quick replies.
// Instagram does not document quick replies on private replies; a rejection
// fails the reply instead of resending it without them.
type PrivateCommentQuickReplyAdapter interface {
	ReplyPrivatelyToCommentWithQuickReplies(ctx context.Context, token, accountID, commentID, message string, quickReplies []QuickReply) (string, error)
}

func (i *InstagramAdapter) ReplyPrivatelyToCommentWithQuickReplies(ctx context.Context, token, accountID, commentID, message string, quickReplies []QuickReply) (string, error) {
	message = strings.TrimSpace(message)
	if accountID == "" || commentID == "" || message == "" || len(message) > 1000 {
		return "", errors.New("private Instagram replies require an account, comment and up to 1000 bytes of text")
	}
	payload := map[string]any{"recipient": map[string]string{"comment_id": commentID}, "message": metaTextMessage(message, quickReplies)}
	body, err := DoJSON(ctx, http.MethodPost, i.graphURL(accountID+"/messages"), payload, bearerHeaders(token))
	if err != nil {
		return "", err
	}
	var response struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}
	if response.MessageID == "" {
		return "", errors.New("the private reply was not confirmed by Instagram; inspect the outcome before sending again")
	}
	return response.MessageID, nil
}

// MessagingUserProfile is what the User Profile API reports about a person
// who messaged the professional account.
type MessagingUserProfile struct {
	Username       string
	FollowsAccount bool
}

// MessagingUserProfileReader reads a messaging user's profile by the ID
// messaging reports for them. Instagram shares it only after that person has
// messaged the account, and never once they blocked it.
type MessagingUserProfileReader interface {
	FetchMessagingUserProfile(ctx context.Context, token, userID string) (MessagingUserProfile, error)
}

// ErrFollowStatusUnreported is a successful read that omitted the follow
// status. It is not the same as "not following".
var ErrFollowStatusUnreported = errors.New("instagram did not report whether this person follows the account")

func (i *InstagramAdapter) FetchMessagingUserProfile(ctx context.Context, token, userID string) (MessagingUserProfile, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return MessagingUserProfile{}, errors.New("the person's Instagram messaging ID is missing")
	}
	query := url.Values{"fields": {"username,is_user_follow_business"}}
	data, err := doMetaRequest(ctx, http.MethodGet, i.graphURL(url.PathEscape(userID))+"?"+query.Encode(), nil, bearerHeaders(token))
	if err != nil {
		// Keep Meta's own status and code: publishing's error mapping would
		// relabel a profile refusal as a media rejection.
		return MessagingUserProfile{}, err
	}
	var response struct {
		Username string `json:"username"`
		Follows  *bool  `json:"is_user_follow_business"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return MessagingUserProfile{}, fmt.Errorf("decoding Instagram user profile: %w", err)
	}
	if response.Follows == nil {
		return MessagingUserProfile{}, ErrFollowStatusUnreported
	}
	return MessagingUserProfile{Username: response.Username, FollowsAccount: *response.Follows}, nil
}
