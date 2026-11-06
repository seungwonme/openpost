package providerreadiness

import (
	"testing"

	"github.com/openpost/backend/internal/capabilities"
	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/providerpolicy"
	"github.com/stretchr/testify/require"
)

// A Unified Login app is the oauth provider entry with a config_id, so its
// accounts resolve to that configuration and need no Page permission to publish.
func TestUnifiedLoginAccountResolvesToTheOAuthConfiguration(t *testing.T) {
	account := models.SocialAccount{
		Platform:        capabilities.ProviderInstagram,
		CapabilityState: `{"connection_type":"unified_login","instagram_account_type":"professional"}`,
	}
	require.Empty(t, accountConfigurationRef(account))

	mode := providerpolicy.Mode(account, capabilities.Capability{Provider: capabilities.ProviderInstagram}, nil)
	require.Equal(t, "instagram.professional", mode)
	require.Equal(t, "professional", providerpolicy.AccountKind(account))

	scopes := RequiredScopesForSubject(Subject{Provider: capabilities.ProviderInstagram, PolicyMode: mode, Operation: OperationPublishImmediate})
	require.Equal(t, []string{"instagram_basic", "instagram_content_publish"}, scopes)
}
