package tokenmanager

import (
	"context"
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/crypto"
	"github.com/stretchr/testify/require"
)

// countingAdapter fails the test by count if the refresh flow is ever reached.
type countingAdapter struct {
	stubAdapter
	refreshes int
}

func (a *countingAdapter) RefreshToken(context.Context, platform.RefreshTokenInput) (*platform.TokenResult, error) {
	a.refreshes++
	return &platform.TokenResult{AccessToken: "must-not-be-used"}, nil
}

func unifiedLoginManager(t *testing.T, expiresAt time.Time) (*TokenManager, *countingAdapter, string) {
	t.Helper()
	db := newGrantSQLiteDB(t)
	encryptor := crypto.NewTokenEncryptor("grant-concurrency-secret")
	seedSharedGrant(t, db, encryptor)
	ctx := t.Context()
	encrypted, err := encryptor.Encrypt("system-user-token")
	require.NoError(t, err)
	query := db.NewUpdate().Model((*models.OAuthGrant)(nil)).
		Set("provider = ?", "instagram").
		Set("access_token_encrypted = ?", encrypted).
		Set("refresh_token_encrypted = NULL").
		Where("id = ?", "grant-shared")
	if expiresAt.IsZero() {
		query = query.Set("access_token_expires_at = NULL")
	} else {
		query = query.Set("access_token_expires_at = ?", expiresAt)
	}
	_, err = query.Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewUpdate().Model((*models.SocialAccount)(nil)).
		Set("platform = ?", "instagram").
		Set("capability_state_json = ?", `{"connection_type":"unified_login"}`).
		Where("oauth_grant_id = ?", "grant-shared").Exec(ctx)
	require.NoError(t, err)
	manager := NewTokenManager(db, encryptor)
	adapter := &countingAdapter{}
	manager.SetProvider(platform.InstagramUnifiedProviderKey, adapter)
	return manager, adapter, "destination-person"
}

func TestUnifiedLoginTokenIsNeverRefreshed(t *testing.T) {
	t.Run("a non-expiring token is returned as stored", func(t *testing.T) {
		manager, adapter, account := unifiedLoginManager(t, time.Time{})
		token, err := manager.GetValidAccessToken(t.Context(), account)
		require.NoError(t, err)
		require.Equal(t, "system-user-token", token)
		require.Zero(t, adapter.refreshes)
	})

	t.Run("a token near expiry stays usable without a refresh", func(t *testing.T) {
		manager, adapter, account := unifiedLoginManager(t, time.Now().Add(time.Minute))
		token, err := manager.GetValidAccessToken(t.Context(), account)
		require.NoError(t, err)
		require.Equal(t, "system-user-token", token)
		require.Zero(t, adapter.refreshes)
	})

	t.Run("an expired token asks for reconnection", func(t *testing.T) {
		manager, adapter, account := unifiedLoginManager(t, time.Now().Add(-time.Minute))
		_, err := manager.GetValidAccessToken(t.Context(), account)
		require.ErrorContains(t, err, "token expired for account")
		require.Zero(t, adapter.refreshes)
	})

	t.Run("a forced refresh after a provider rejection fails without a call", func(t *testing.T) {
		manager, adapter, account := unifiedLoginManager(t, time.Time{})
		_, err := manager.ForceRefreshAccessToken(t.Context(), account)
		require.ErrorContains(t, err, "not supported")
		require.Zero(t, adapter.refreshes)
	})

	t.Run("the durable job ends instead of retrying", func(t *testing.T) {
		manager, adapter, _ := unifiedLoginManager(t, time.Now().Add(time.Minute))
		_, err := manager.ForceRefreshGrant(t.Context(), "grant-shared")
		require.True(t, IsObsoleteRefreshTarget(err), "got %v", err)
		require.Zero(t, adapter.refreshes)
	})
}

func TestForceRefreshGrantStillRefreshesSupportedProviders(t *testing.T) {
	db := newGrantSQLiteDB(t)
	encryptor := crypto.NewTokenEncryptor("grant-concurrency-secret")
	seedSharedGrant(t, db, encryptor)
	manager := NewTokenManager(db, encryptor)
	manager.SetProvider("linkedin", &stubAdapter{
		capability: platform.RefreshCapability{Supported: true, CredentialSource: platform.RefreshCredentialRefreshToken},
		tokenResp:  &platform.TokenResult{AccessToken: "fresh", ExpiresIn: 3600},
	})
	token, err := manager.ForceRefreshGrant(t.Context(), "grant-shared")
	require.NoError(t, err)
	require.Equal(t, "fresh", token)
}
