package models

import (
	"time"

	"github.com/uptrace/bun"
)

// InstagramWebhookSubscription records the latest attempt to subscribe one
// connected Instagram account to Meta webhooks. Messages are fixed, safe
// strings; provider responses and tokens are never stored.
type InstagramWebhookSubscription struct {
	bun.BaseModel `bun:"table:instagram_webhook_subscriptions,alias:iws"`

	SocialAccountID string    `bun:"social_account_id,pk" json:"social_account_id"`
	WorkspaceID     string    `bun:"workspace_id,notnull" json:"workspace_id"`
	Status          string    `bun:",notnull" json:"status"`
	ErrorCode       string    `bun:"error_code,notnull,default:''" json:"error_code"`
	ErrorMessage    string    `bun:"error_message,notnull,default:''" json:"error_message"`
	Fields          string    `bun:",notnull,default:''" json:"fields"`
	LastAttemptedAt time.Time `bun:"last_attempted_at,nullzero" json:"last_attempted_at"`
	LastSuccessAt   time.Time `bun:"last_success_at,nullzero" json:"last_success_at"`
	CreatedAt       time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"-"`
	UpdatedAt       time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"-"`
}
