package engagement

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/openpost/backend/internal/jobregistry"
	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/organizationguard"
	"github.com/openpost/backend/internal/services/providerwrite"
	"github.com/uptrace/bun"
)

var errActivityChanged = errors.New("account activity authority or checkpoint changed")
var errActivityInactive = errors.New("account activity is disabled")

type accountActivityJob struct {
	AccountID  string `json:"account_id"`
	Feed       string `json:"feed"`
	Generation string `json:"generation"`
}

type activityAuthority struct {
	Account          models.SocialAccount
	FeatureDecidedAt time.Time
	GrantConsentedAt time.Time
}

func (s *Service) SetActivityProvider(name string, adapter platform.AccountActivityAdapter) {
	s.providersMu.Lock()
	defer s.providersMu.Unlock()
	s.activityProviders[name] = adapter
}

func (s *Service) activityAdapter(account models.SocialAccount) platform.AccountActivityAdapter {
	if account.Platform == "x" {
		return nil
	}
	s.providersMu.RLock()
	defer s.providersMu.RUnlock()
	return s.activityProviders[engagementProviderKey(account)]
}

func activityFeed(adapter platform.AccountActivityAdapter, feed string) (platform.AccountActivitySupport, bool) {
	if adapter != nil {
		for _, support := range adapter.AccountActivitySupport() {
			if support.Feed == feed {
				return support, true
			}
		}
	}
	return platform.AccountActivitySupport{}, false
}

func (s *Service) refreshAccountActivity(ctx context.Context, workspaceID string, force bool) (int, error) {
	s.providersMu.RLock()
	configured := len(s.activityProviders) > 0
	s.providersMu.RUnlock()
	if !configured {
		return 0, nil
	}
	var accounts []models.SocialAccount
	if err := s.db.NewSelect().Model(&accounts).Where("workspace_id=? AND is_active=?", workspaceID, true).Order("id ASC").Scan(ctx); err != nil {
		return 0, err
	}
	queued := 0
	for _, account := range accounts {
		adapter := s.activityAdapter(account)
		if adapter == nil {
			continue
		}
		for _, support := range adapter.AccountActivitySupport() {
			state, err := s.prepareActivityState(ctx, account, support)
			if errors.Is(err, errActivityInactive) {
				continue
			}
			if err != nil {
				return queued, err
			}
			if state.Status == "permission_required" || (!force && state.NextSyncAt.After(s.now())) {
				continue
			}
			payload, _ := json.Marshal(accountActivityJob{AccountID: account.ID, Feed: support.Feed, Generation: state.Generation})
			inserted, err := s.enqueue(ctx, workspaceID, jobregistry.TypeEngagementAccountSync, string(payload), s.now())
			if err != nil {
				return queued, err
			}
			if inserted {
				queued++
			}
		}
	}
	return queued, nil
}

func (s *Service) loadActivityAuthority(ctx context.Context, db bun.IDB, accountID string) (activityAuthority, error) {
	var authority activityAuthority
	if err := db.NewSelect().Model(&authority.Account).Where("id=? AND is_active=?", accountID, true).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authority, errActivityInactive
		}
		return authority, err
	}
	enabled := false
	if gate, ok := s.featureGate.(interface {
		IsEffectiveEnabledWithDB(context.Context, bun.IDB, string, string) (bool, error)
	}); ok {
		var err error
		enabled, err = gate.IsEffectiveEnabledWithDB(ctx, db, accountID, "engagement")
		if err != nil {
			return authority, err
		}
	} else {
		enabled = s.isEngagementEnabled(ctx, accountID)
	}
	if !enabled {
		return authority, errActivityInactive
	}
	var feature models.AccountFeature
	err := db.NewSelect().Model(&feature).Where("social_account_id=? AND feature='engagement'", accountID).Scan(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return authority, err
	}
	authority.FeatureDecidedAt = feature.DecidedAt
	if authority.Account.OAuthGrantID != "" {
		var grant models.OAuthGrant
		if err := db.NewSelect().Model(&grant).Where("id=? AND workspace_id=?", authority.Account.OAuthGrantID, authority.Account.WorkspaceID).Scan(ctx); err != nil {
			return authority, errActivityInactive
		}
		if !grant.RevokedAt.IsZero() {
			return authority, errActivityInactive
		}
		authority.GrantConsentedAt = grant.ConsentedAt
		authority.Account.GrantedScopes = grant.GrantedScopes
	}
	return authority, nil
}

func (s *Service) prepareActivityState(ctx context.Context, account models.SocialAccount, support platform.AccountActivitySupport) (models.EngagementAccountSyncState, error) {
	var state models.EngagementAccountSyncState
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := organizationguard.LockWorkspace(ctx, tx, account.WorkspaceID); err != nil {
			return err
		}
		authority, err := s.loadActivityAuthority(ctx, tx, account.ID)
		if err != nil {
			return err
		}
		err = tx.NewSelect().Model(&state).Where("social_account_id=? AND feed=?", account.ID, support.Feed).Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if state.Generation == "" || state.GrantID != authority.Account.OAuthGrantID || !state.GrantConsentedAt.Equal(authority.GrantConsentedAt) || !state.FeatureDecidedAt.Equal(authority.FeatureDecidedAt) || state.Status == "disabled" {
			state.Generation = uuid.NewString()
			state.Cursor, state.CycleStartedAt, state.PendingWatermark = "", time.Time{}, time.Time{}
			state.NextSyncAt = time.Time{}
			state.Status, state.ErrorCode, state.ErrorMessage = "pending", "", ""
		}
		state.SocialAccountID, state.WorkspaceID, state.Platform, state.Feed = account.ID, account.WorkspaceID, account.Platform, support.Feed
		state.GrantID, state.GrantConsentedAt, state.FeatureDecidedAt = authority.Account.OAuthGrantID, authority.GrantConsentedAt, authority.FeatureDecidedAt
		if state.Status == "permission_required" && len(platform.MissingAnalyticsScopes(authority.Account.GrantedScopes, support.RequiredScopes)) == 0 {
			state.Status, state.ErrorCode, state.ErrorMessage = "pending", "", ""
			state.NextSyncAt = time.Time{}
		}
		if len(platform.MissingAnalyticsScopes(authority.Account.GrantedScopes, support.RequiredScopes)) > 0 {
			state.Status, state.ErrorCode, state.ErrorMessage = "permission_required", "missing_scope", "Reconnect this account and grant access to this activity feed."
		}
		state.UpdatedAt = s.now()
		_, err = tx.NewInsert().Model(&state).On("CONFLICT (social_account_id,feed) DO UPDATE").
			Set("generation=EXCLUDED.generation").Set("grant_id=EXCLUDED.grant_id").Set("grant_consented_at=EXCLUDED.grant_consented_at").Set("feature_decided_at=EXCLUDED.feature_decided_at").
			Set("cursor=EXCLUDED.cursor").Set("cycle_started_at=EXCLUDED.cycle_started_at").Set("pending_watermark=EXCLUDED.pending_watermark").
			Set("status=EXCLUDED.status").Set("error_code=EXCLUDED.error_code").Set("error_message=EXCLUDED.error_message").Set("next_sync_at=EXCLUDED.next_sync_at").Set("updated_at=EXCLUDED.updated_at").Exec(ctx)
		return err
	})
	return state, err
}

func (s *Service) syncAccountActivity(ctx context.Context, input accountActivityJob) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	state, authority, _, err := s.activityCheckpoint(ctx, s.db, input)
	if errors.Is(err, errActivityChanged) || errors.Is(err, errActivityInactive) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.tokenSource == nil {
		return s.recordActivityFailure(ctx, state, fmt.Errorf("account token source unavailable"))
	}
	token, err := s.tokenSource.GetValidAccessToken(ctx, input.AccountID)
	if err != nil {
		return s.recordActivityFailure(ctx, state, &platform.HTTPError{StatusCode: http.StatusUnauthorized})
	}
	if input.Feed == platform.InstagramCommentsFeed {
		return s.collectInstagramNativeComments(ctx, input, token)
	}
	var pending []string
	if input.Feed == "reviews" {
		pending, err = s.dueReviewReplies(ctx, input.AccountID)
		if err != nil {
			return err
		}
	}
	collectionErr := s.collectAccountActivity(ctx, input, token)
	if input.Feed == "reviews" {
		changed, err := s.dueReviewReplies(ctx, input.AccountID)
		if err != nil {
			return errors.Join(collectionErr, err)
		}
		pending = reviewRecheckBatch(pending, changed)
		if err := s.recheckReviewReplies(ctx, authority, token, input, pending); err != nil {
			if errors.Is(err, errActivityChanged) || errors.Is(err, errActivityInactive) {
				return collectionErr
			}
			return errors.Join(collectionErr, s.recordActivityFailure(ctx, state, err))
		}
	}
	return collectionErr
}

func (s *Service) collectAccountActivity(ctx context.Context, input accountActivityJob, token string) error {
	seen := map[string]bool{}
	for range 3 {
		state, authority, adapter, err := s.activityCheckpoint(ctx, s.db, input)
		if errors.Is(err, errActivityChanged) || errors.Is(err, errActivityInactive) {
			return nil
		}
		if err != nil {
			return err
		}
		if state.CycleStartedAt.IsZero() {
			state.CycleStartedAt = s.now()
			state.PendingWatermark = state.CycleStartedAt
		}
		since := state.Watermark
		if since.IsZero() {
			since = state.CreatedAt.Add(-30 * 24 * time.Hour)
		}
		page, err := adapter.ListAccountActivity(ctx, token, authority.Account.AccountID, platform.AccountActivityRequest{Feed: input.Feed, Cursor: state.Cursor, Since: since.Add(-time.Minute)})
		if err != nil {
			return s.recordActivityFailure(ctx, state, err)
		}
		seen[state.Cursor] = true
		if page.NextCursor != "" && seen[page.NextCursor] {
			return s.recordActivityFailure(ctx, state, fmt.Errorf("provider repeated an account activity cursor"))
		}
		if err := s.commitActivityPage(ctx, input, authority, state, page, since.Add(-time.Minute)); err != nil {
			if errors.Is(err, errActivityChanged) || errors.Is(err, errActivityInactive) {
				return nil
			}
			return err
		}
		if page.NextCursor == "" {
			return nil
		}
	}
	return nil
}

func (s *Service) activityCheckpoint(ctx context.Context, db bun.IDB, input accountActivityJob) (models.EngagementAccountSyncState, activityAuthority, platform.AccountActivityAdapter, error) {
	var state models.EngagementAccountSyncState
	authority, err := s.loadActivityAuthority(ctx, db, input.AccountID)
	if err != nil {
		return state, authority, nil, err
	}
	adapter := s.activityAdapter(authority.Account)
	support, found := activityFeed(adapter, input.Feed)
	if !found || len(platform.MissingAnalyticsScopes(authority.Account.GrantedScopes, support.RequiredScopes)) > 0 {
		return state, authority, adapter, errActivityChanged
	}
	if err := db.NewSelect().Model(&state).Where("social_account_id=? AND feed=?", input.AccountID, input.Feed).Scan(ctx); err != nil {
		return state, authority, adapter, err
	}
	if state.Generation != input.Generation || state.GrantID != authority.Account.OAuthGrantID || !state.GrantConsentedAt.Equal(authority.GrantConsentedAt) || !state.FeatureDecidedAt.Equal(authority.FeatureDecidedAt) {
		return state, authority, adapter, errActivityChanged
	}
	if err := providerwrite.RequireCurrentJobExecution(ctx, db, ""); err != nil {
		return state, authority, adapter, errActivityChanged
	}
	return state, authority, adapter, nil
}

func (s *Service) recordActivityFailure(ctx context.Context, state models.EngagementAccountSyncState, failure error) error {
	status, code, message := "failed", "provider_unavailable", "Could not collect account activity. Try refreshing again."
	var provider *platform.HTTPError
	if errors.As(failure, &provider) {
		switch provider.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			status, code, message = "permission_required", "missing_scope", "Reconnect this account and grant access to this activity feed."
		case 429:
			status, code, message = "rate_limited", "provider_rate_limit", "Activity collection is rate limited. Try again later."
		}
	}
	_, err := s.db.NewUpdate().Model((*models.EngagementAccountSyncState)(nil)).Set("status=?", status).Set("error_code=?", code).Set("error_message=?", message).
		Set("last_attempted_at=?", s.now()).Set("next_sync_at=?", s.now().Add(15*time.Minute)).Set("updated_at=?", s.now()).Where("social_account_id=? AND feed=? AND generation=?", state.SocialAccountID, state.Feed, state.Generation).Exec(ctx)
	return err
}

// Two reads per cycle keep moderation polling within the account read budget.
func reviewRecheckBatch(previous, changed []string) []string {
	batch := make([]string, 0, 2)
	for _, id := range append(append([]string{}, previous...), changed...) {
		if len(batch) == 2 {
			break
		}
		found := false
		for _, value := range batch {
			if value == id {
				found = true
			}
		}
		if !found {
			batch = append(batch, id)
		}
	}
	return batch
}
