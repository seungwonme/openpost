package platform

import (
	"context"
	"time"
)

type MessagingSupport struct {
	Enabled           bool
	RequiredScopes    []string
	CanSend           bool
	RequiresOptIn     bool
	ReplyWindow       time.Duration
	ConversationModel string
	Unavailable       string
}

type MessageAttachment struct {
	Type      string `json:"type"`
	URL       string `json:"url"`
	Name      string `json:"name,omitempty"`
	MimeType  string `json:"mime_type,omitempty"`
	Thumbnail string `json:"thumbnail,omitempty"`
}

type ProviderMessage struct {
	// ReactionsKnown distinguishes a complete provider snapshot from omitted data.
	ReactionsKnown  bool
	Reactions       []ProviderMessageReaction
	ID              string
	Direction       string
	AuthorRemoteID  string
	Body            string
	Attachments     []MessageAttachment
	RemoteCreatedAt time.Time
}

type ProviderConversation struct {
	// Partial marks event-derived metadata: preserve absent fields, unread state,
	// and newer last-message values when applying delayed webhook receipts.
	Partial              bool
	ID                   string
	CounterpartRemoteID  string
	CounterpartName      string
	CounterpartHandle    string
	CounterpartAvatarURL string
	LastMessageAt        time.Time
	LastMessagePreview   string
	LastRemoteMessageID  string
	UnreadCountKnown     bool
	UnreadCount          int
	ReplyWindowExpiresAt time.Time
	Messages             []ProviderMessage
}

type FetchMessagesRequest struct {
	AccountID, Cursor string
	Limit             int
}
type FetchMessagesResult struct {
	Conversations []ProviderConversation
	NextCursor    string
}
type SendMessageRequest struct {
	AccountID            string
	RemoteConversationID string
	CounterpartRemoteID  string
	CounterpartHandle    string
	ReplyToRemoteID      string
	Body                 string
	// Buttons turn the message into a Meta button template. Empty keeps the
	// request, and its write fingerprint, unchanged.
	Buttons []MessageButton `json:",omitempty"`
}
type SendMessageResult struct {
	RemoteMessageID string
	CreatedAt       time.Time
}

// MessagingAdapter is the optional provider seam used by Messaging.
type MessagingAdapter interface {
	MessagingSupport() MessagingSupport
	FetchMessages(context.Context, string, FetchMessagesRequest) (FetchMessagesResult, error)
	SendMessage(context.Context, string, SendMessageRequest) (SendMessageResult, error)
}

// IncrementalMessagingAdapter reads an account event log independently of
// conversation discovery and message history. Commit the whole batch and its
// NextCursor in one transaction; failed fetches never advance a checkpoint.
type IncrementalMessagingAdapter interface {
	FetchMessageEvents(context.Context, string, FetchMessageEventsRequest) (FetchMessageEventsResult, error)
}

type FetchMessageEventsRequest struct {
	AccountID, Cursor string
	Limit             int
}

type DeletedProviderMessage struct {
	ConversationID, MessageID string
}

type ProviderReadState struct {
	ConversationID, MessageID, ActorRemoteID string
	ReadAt                                   time.Time
}

type ProviderMessageReaction struct {
	ConversationID, MessageID, ActorRemoteID, Value string
	Deleted                                         bool
}

type FetchMessageEventsResult struct {
	Conversations        []ProviderConversation
	DeletedMessages      []DeletedProviderMessage
	DeletedConversations []string
	ReadStates           []ProviderReadState
	Reactions            []ProviderMessageReaction
	NextCursor           string
	HasMore              bool
	// Bootstrap captures the current log head without treating historical log
	// entries as new messages. Discover the inbox, then replay from this cursor.
	Bootstrap bool
}

// MessageHistoryAdapter fetches older messages for one known conversation.
// Its cursor must never be stored in the account event-log checkpoint.
type MessageHistoryAdapter interface {
	FetchMessageHistory(context.Context, string, FetchMessageHistoryRequest) (FetchMessageHistoryResult, error)
}

type FetchMessageHistoryRequest struct {
	AccountID, ConversationID, Cursor string
	Limit                             int
}

type FetchMessageHistoryResult struct {
	Messages        []ProviderMessage
	DeletedMessages []DeletedProviderMessage
	NextCursor      string
}
