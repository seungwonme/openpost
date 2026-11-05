// Package instagramwebhook subscribes connected Instagram accounts to Meta
// webhooks and records the outcome for the inbox readiness surface.
//
// Meta sends comment and message events for an account only after the app
// subscribes that account through the /subscribed_apps edge. The inbound route
// lives in cmd/openpost/inbox_runtime.go; this package owns the outbound half.
package instagramwebhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
)

const (
	StatusSubscribed = "subscribed"
	StatusFailed     = "failed"

	// subscribeTimeout bounds the Meta call so a feature toggle never hangs.
	subscribeTimeout = 10 * time.Second
)

// Feature names mirror accountfeatures. They are duplicated, not imported, so
// accountfeatures can depend on this package's seam without a cycle.
var subscribingFeatures = []string{"engagement", "messaging"}

type TokenSource interface {
	GetValidAccessToken(ctx context.Context, accountID string) (string, error)
}

type Service struct {
	db          bun.IDB
	tokens      TokenSource
	providersMu sync.RWMutex
	providers   map[string]platform.Adapter
	now         func() time.Time
}

func NewService(db bun.IDB, tokens TokenSource, providers map[string]platform.Adapter) *Service {
	snapshot := make(map[string]platform.Adapter, len(providers))
	for name, adapter := range providers {
		snapshot[name] = adapter
	}
	return &Service{db: db, tokens: tokens, providers: snapshot, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) SetProvider(name string, adapter platform.Adapter) {
	s.providersMu.Lock()
	defer s.providersMu.Unlock()
	s.providers[name] = adapter
}

func (s *Service) subscriber(key string) platform.InstagramWebhookSubscriber {
	s.providersMu.RLock()
	defer s.providersMu.RUnlock()
	subscriber, _ := s.providers[key].(platform.InstagramWebhookSubscriber)
	return subscriber
}

// Ensure subscribes the account and records the outcome. Meta treats a repeat
// subscription as a no-op success, so every call posts again: a reconnect
// replaces the token and an earlier failure must be retried. A provider
// failure is recorded and returned; callers must not let it fail their own
// operation.
func (s *Service) Ensure(ctx context.Context, accountID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	var account models.SocialAccount
	if err := s.db.NewSelect().Model(&account).Where("id = ?", accountID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if account.Platform != "instagram" || !account.IsActive {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, subscribeTimeout)
	defer cancel()
	subErr := s.subscribe(ctx, account)
	if subErr == nil {
		return s.record(ctx, account, StatusSubscribed, platform.InstagramWebhookFailure{})
	}
	failure := platform.ClassifyInstagramWebhookError(subErr)
	// Log only fixed fields. The error carries no token or response body.
	log.Printf("[Instagram Webhook] subscription failed account=%q code=%q", account.ID, failure.Code)
	if err := s.record(ctx, account, StatusFailed, failure); err != nil {
		return errors.Join(subErr, err)
	}
	return subErr
}

func (s *Service) subscribe(ctx context.Context, account models.SocialAccount) error {
	subscriber := s.subscriber(platform.AccountProviderKey(account.Platform, account.InstanceURL, account.CapabilityState))
	if subscriber == nil {
		return platform.ErrInstagramWebhookUnsupported
	}
	state := map[string]string{}
	_ = json.Unmarshal([]byte(account.CapabilityState), &state)
	if s.tokens == nil {
		return platform.ErrInstagramWebhookUnsupported
	}
	token, err := s.tokens.GetValidAccessToken(ctx, account.ID)
	if err != nil {
		return &platform.HTTPError{StatusCode: 401}
	}
	return subscriber.SubscribeInstagramWebhooks(ctx, platform.InstagramWebhookSubscription{
		AccessToken: token, AccountID: account.AccountID, PageID: state["facebook_page_id"],
	})
}

// EnsureIfEnabled is the reconnect hook: it subscribes only accounts whose
// Comments or Direct messages feature is on.
func (s *Service) EnsureIfEnabled(ctx context.Context, accountID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	enabled, err := s.db.NewSelect().Model((*models.AccountFeature)(nil)).
		Where("social_account_id = ? AND enabled = ? AND feature IN (?)", accountID, true, bun.List(subscribingFeatures)).
		Exists(ctx)
	if err != nil || !enabled {
		return err
	}
	return s.Ensure(ctx, accountID)
}

func (s *Service) record(ctx context.Context, account models.SocialAccount, status string, failure platform.InstagramWebhookFailure) error {
	now := s.now()
	row := &models.InstagramWebhookSubscription{
		SocialAccountID: account.ID, WorkspaceID: account.WorkspaceID, Status: status,
		ErrorCode: failure.Code, ErrorMessage: failure.Message, Fields: platform.InstagramWebhookFields,
		LastAttemptedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	query := s.db.NewInsert().Model(row).On("CONFLICT (social_account_id) DO UPDATE").
		Set("workspace_id = EXCLUDED.workspace_id").
		Set("status = EXCLUDED.status").
		Set("error_code = EXCLUDED.error_code").
		Set("error_message = EXCLUDED.error_message").
		Set("fields = EXCLUDED.fields").
		Set("last_attempted_at = EXCLUDED.last_attempted_at").
		Set("updated_at = EXCLUDED.updated_at")
	if status == StatusSubscribed {
		row.LastSuccessAt = now
		query = query.Set("last_success_at = EXCLUDED.last_success_at")
	}
	_, err := query.Exec(ctx)
	return err
}

// Failure is a failed subscription as the inbox sync-state surfaces show it.
// Successful subscriptions are not reported: they are not a collection run
// and must not move the inbox's last-successful-sync time.
type Failure struct {
	ID              string
	WorkspaceID     string
	SocialAccountID string
	Status          string
	ErrorCode       string
	ErrorMessage    string
	LastAttemptedAt time.Time
}

// Failures lists failed subscriptions for accounts that still have the given
// feature on ("engagement" for Comments, "messaging" for Messages), so a
// disabled feature leaves no stale failure behind.
func Failures(ctx context.Context, db bun.IDB, workspaceID, feature string) ([]Failure, error) {
	var rows []models.InstagramWebhookSubscription
	err := db.NewSelect().Model(&rows).
		Where("iws.workspace_id = ? AND iws.status = ?", workspaceID, StatusFailed).
		Where("EXISTS (SELECT 1 FROM account_features AS f WHERE f.social_account_id = iws.social_account_id AND f.enabled = ? AND f.feature = ?)", true, feature).
		Order("iws.social_account_id ASC").
		Scan(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	failures := make([]Failure, 0, len(rows))
	for _, row := range rows {
		failures = append(failures, Failure{
			ID: "instagram-webhook:" + row.SocialAccountID, WorkspaceID: row.WorkspaceID,
			SocialAccountID: row.SocialAccountID, Status: failureStatus(row.ErrorCode),
			ErrorCode: row.ErrorCode, ErrorMessage: strings.TrimSpace(row.ErrorMessage),
			LastAttemptedAt: row.LastAttemptedAt,
		})
	}
	return failures, nil
}

func failureStatus(code string) string {
	switch code {
	case platform.InstagramWebhookRateLimited:
		return "rate_limited"
	case platform.InstagramWebhookUnavailable:
		return "temporarily_unavailable"
	case platform.InstagramWebhookAuthRequired, platform.InstagramWebhookPermissionDenied:
		return "permission_required"
	}
	return "error"
}

// EngagementStates shapes Comments failures for the engagement inbox.
func EngagementStates(ctx context.Context, db bun.IDB, workspaceID string) ([]models.EngagementSyncState, error) {
	failures, err := Failures(ctx, db, workspaceID, "engagement")
	if err != nil {
		return nil, err
	}
	states := make([]models.EngagementSyncState, 0, len(failures))
	for _, f := range failures {
		states = append(states, models.EngagementSyncState{ID: f.ID, Feed: "comments", WorkspaceID: f.WorkspaceID,
			SocialAccountID: f.SocialAccountID, Platform: "instagram", Status: f.Status, ErrorCode: f.ErrorCode,
			ErrorMessage: f.ErrorMessage, LastAttemptedAt: f.LastAttemptedAt})
	}
	return states, nil
}

// MessagingStates shapes Direct message failures for the Messages inbox.
func MessagingStates(ctx context.Context, db bun.IDB, workspaceID string) ([]models.MessagingSyncState, error) {
	failures, err := Failures(ctx, db, workspaceID, "messaging")
	if err != nil {
		return nil, err
	}
	states := make([]models.MessagingSyncState, 0, len(failures))
	for _, f := range failures {
		states = append(states, models.MessagingSyncState{ID: f.ID, WorkspaceID: f.WorkspaceID,
			SocialAccountID: f.SocialAccountID, Platform: "instagram", Status: f.Status, ErrorCode: f.ErrorCode,
			ErrorMessage: f.ErrorMessage, LastAttemptedAt: f.LastAttemptedAt})
	}
	return states, nil
}

// ReconnectHook adapts the service to the account saver. It resubscribes each
// saved Instagram account whose features are on, off the request path so the
// connect redirect never waits on Meta.
func ReconnectHook(s *Service) func(context.Context, []*models.SocialAccount) {
	return func(ctx context.Context, accounts []*models.SocialAccount) {
		ids := make([]string, 0, len(accounts))
		for _, account := range accounts {
			if account != nil && account.Platform == "instagram" {
				ids = append(ids, account.ID)
			}
		}
		if len(ids) == 0 {
			return
		}
		detached := context.WithoutCancel(ctx)
		go func() {
			for _, id := range ids {
				_ = s.EnsureIfEnabled(detached, id)
			}
		}()
	}
}
