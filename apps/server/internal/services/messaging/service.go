package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/openpost/backend/internal/jobregistry"
	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/notifications"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/uptrace/bun"
)

const (
	JobTypeSweep        = jobregistry.TypeMessagingSweep
	JobTypeMessagesSync = jobregistry.TypeMessagesSync
	JobTypeMessageSend  = jobregistry.TypeMessageSend
	sweepInterval       = 5 * time.Minute
)

var (
	ErrOperationConflict = errors.New("client operation ID was already used for another message")
	ErrAccessDenied      = errors.New("workspace access denied")
	ErrNotFound          = errors.New("messaging resource not found")
)

type Actor = workspaceaccess.ActorFacts

type TokenSource interface {
	GetValidAccessToken(ctx context.Context, accountID string) (string, error)
}

type Provider interface {
	platform.MessagingAdapter
}

type FeatureGate interface {
	IsEffectiveEnabled(ctx context.Context, accountID, feature string) (bool, error)
}

type Service struct {
	messagingTokens       SupplementalMessagingTokens
	messagingNotConnected error
	db                    *bun.DB
	tokens                TokenSource
	notifications         *notifications.Service
	states                stateRepository
	providersMu           sync.RWMutex
	providers             map[string]Provider
	receiptDrainer        func(context.Context) error
	now                   func() time.Time
	featureGate           FeatureGate
	automationRunStopper  func(context.Context, bun.Tx, string, []string, string) error
}

func NewService(db *bun.DB, tokens TokenSource, notificationService *notifications.Service) *Service {
	return &Service{
		db: db, tokens: tokens, notifications: notificationService,
		states: newStateRepository(db), providers: make(map[string]Provider),
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetProvider(name string, provider Provider) {
	s.providersMu.Lock()
	defer s.providersMu.Unlock()
	s.providers[strings.ToLower(strings.TrimSpace(name))] = provider
}

func (s *Service) SetFeatureGate(g FeatureGate) {
	s.featureGate = g
}

func (s *Service) isMessagingEnabled(ctx context.Context, accountID string) bool {
	if s.featureGate == nil {
		return false
	}
	enabled, err := s.featureGate.IsEffectiveEnabled(ctx, accountID, "messaging")
	if err != nil {
		return false
	}
	return enabled
}

func (s *Service) provider(account models.SocialAccount) Provider {
	s.providersMu.RLock()
	defer s.providersMu.RUnlock()
	return s.providers[providerKey(account)]
}

func (s *Service) authorize(ctx context.Context, workspaceID string, actor Actor, level workspaceaccess.Level) error {
	decision, err := workspaceaccess.NewAuthorizer(s.db).Authorize(ctx, workspaceID, actor, level)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return ErrAccessDenied
	}
	return nil
}

func (s *Service) ScheduleSweep(ctx context.Context, runAt time.Time) error {
	payload, _ := json.Marshal(map[string]string{"scheduled_for": runAt.UTC().Truncate(time.Minute).Format(time.RFC3339)})
	_, err := s.enqueue(ctx, "", JobTypeSweep, string(payload), runAt)
	return err
}

func (s *Service) HandleJob(ctx context.Context, jobType, payload string) error {
	var input subjectJob
	switch jobType {
	case JobTypeSweep:
		return s.handleSweep(ctx)
	case JobTypeMessagesSync, JobTypeMessageSend:
		if err := json.Unmarshal([]byte(payload), &input); err != nil {
			return fmt.Errorf("decode messaging job: %w", err)
		}
		if jobType == JobTypeMessagesSync && strings.HasPrefix(input.ID, "provider-receipts:") {
			if s.receiptDrainer == nil {
				return errors.New("inbox receipt processor is unavailable")
			}
			return s.receiptDrainer(ctx)
		}
		if jobType == JobTypeMessagesSync {
			if input.ConversationID != "" {
				return s.syncHistory(ctx, input.ID, input.ConversationID)
			}
			return s.syncMessages(ctx, input.ID)
		}
		return s.sendMessage(ctx, input.ID)
	default:
		return fmt.Errorf("unsupported messaging job type %q", jobType)
	}
}

type subjectJob struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id,omitempty"`
}

func (s *Service) handleSweep(ctx context.Context) error {
	if s.receiptDrainer != nil {
		if err := s.receiptDrainer(ctx); err != nil {
			return err
		}
	}
	var workspaces []string
	if err := s.db.NewSelect().Model((*models.SocialAccount)(nil)).
		ColumnExpr("DISTINCT workspace_id").Where("is_active = ?", true).Scan(ctx, &workspaces); err != nil {
		return err
	}
	for _, workspaceID := range workspaces {
		if _, err := s.refreshWorkspace(ctx, workspaceID, false); err != nil {
			return err
		}
	}
	return s.ScheduleSweep(ctx, s.now().Add(sweepInterval))
}

func (s *Service) RefreshWorkspace(ctx context.Context, actor Actor, workspaceID string, force bool) (int, error) {
	if err := s.authorize(ctx, workspaceID, actor, workspaceaccess.LevelEdit); err != nil {
		return 0, err
	}
	return s.refreshWorkspace(ctx, workspaceID, force)
}

// ActiveRefreshJobIDs returns the durable collection receipts a manual refresh
// can wait on. Admission coalesces callers onto the same account/post job.
func (s *Service) ActiveRefreshJobIDs(ctx context.Context, actor Actor, workspaceID string) ([]string, error) {
	if err := s.authorize(ctx, workspaceID, actor, workspaceaccess.LevelEdit); err != nil {
		return nil, err
	}
	ids := []string{}
	err := s.db.NewSelect().Model((*models.Job)(nil)).Column("id").
		Where("scope_id=? AND type=? AND status IN ('pending','processing')", workspaceID, JobTypeMessagesSync).
		Order("created_at", "id").Scan(ctx, &ids)
	return ids, err
}

func (s *Service) refreshWorkspace(ctx context.Context, workspaceID string, force bool) (int, error) {
	var accounts []models.SocialAccount
	if err := s.db.NewSelect().Model(&accounts).Where("workspace_id = ? AND is_active = ?", workspaceID, true).Scan(ctx); err != nil {
		return 0, err
	}
	now := s.now()
	queued := 0
	for _, account := range accounts {
		if !s.isMessagingEnabled(ctx, account.ID) {
			_ = s.states.record(ctx, syncStateUpdate{
				account: account, status: syncStateDisabled,
				failure:     syncStateFailure{code: "opt_in_required", message: "Enable messaging for this account to collect messages."},
				attemptedAt: now,
			})
			continue
		}
		provider := s.provider(account)
		if provider == nil || !provider.MessagingSupport().Enabled {
			continue
		}
		support := provider.MessagingSupport()
		if missing := platform.MissingAnalyticsScopes(account.GrantedScopes, support.RequiredScopes); len(missing) > 0 {
			_ = s.states.record(ctx, syncStateUpdate{
				account: account, status: syncStatePermissionRequired,
				failure: syncStateFailure{code: "missing_scope", message: "Reconnect this account and grant messaging access."},
				cadence: 24 * time.Hour, attemptedAt: now,
			})
			continue
		}
		if !force && !s.states.due(ctx, account.ID, now) {
			continue
		}
		payload, _ := json.Marshal(subjectJob{ID: account.ID})
		inserted, err := s.enqueue(ctx, workspaceID, JobTypeMessagesSync, string(payload), now)
		if err != nil {
			return queued, err
		}
		if inserted {
			queued++
		}
	}
	return queued, nil
}

func (s *Service) enqueue(ctx context.Context, workspaceID, jobType, payload string, runAt time.Time) (bool, error) {
	job, err := jobregistry.NewJob(jobType, payload, runAt)
	if err != nil {
		return false, err
	}
	job.ScopeID = workspaceID
	result, err := s.db.NewInsert().Model(job).On("CONFLICT DO NOTHING").Exec(ctx)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if workspaceID != "" {
		_, err = s.db.NewUpdate().Model((*models.Job)(nil)).Set("scope_id=?", workspaceID).
			Where("type=? AND payload=? AND scope_id='' AND status IN ('pending','processing')", jobType, payload).Exec(ctx)
	}
	return rows == 1, err
}

func providerKey(account models.SocialAccount) string {
	key := strings.ToLower(strings.TrimSpace(account.Platform))
	return platform.AccountProviderKey(key, account.InstanceURL, account.CapabilityState)
}

func (s *Service) SetReceiptDrainer(drain func(context.Context) error) { s.receiptDrainer = drain }
func (s *Service) ScheduleReceiptDrain(ctx context.Context) error {
	payload, _ := json.Marshal(subjectJob{ID: "provider-receipts:pending"})
	_, err := s.enqueue(ctx, "", JobTypeMessagesSync, string(payload), s.now())
	return err
}

func (s *Service) isMessagingEnabledWithDB(ctx context.Context, db bun.IDB, accountID string) bool {
	if scoped, ok := s.featureGate.(interface {
		IsEffectiveEnabledWithDB(context.Context, bun.IDB, string, string) (bool, error)
	}); ok {
		enabled, err := scoped.IsEffectiveEnabledWithDB(ctx, db, accountID, "messaging")
		return err == nil && enabled
	}
	return s.isMessagingEnabled(ctx, accountID)
}
