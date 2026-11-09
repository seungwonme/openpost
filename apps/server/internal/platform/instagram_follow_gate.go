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

const (
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
		return map[string]any{"text": text}
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
