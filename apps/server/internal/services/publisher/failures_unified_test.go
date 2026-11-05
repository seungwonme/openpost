package publisher

import (
	"errors"
	"fmt"
	"testing"

	"github.com/openpost/backend/internal/platform"
	"github.com/stretchr/testify/require"
)

// A Unified Login token has no refresh flow. Each way it can stop working must
// reach the person as a reconnect, not as a retried publish.
func TestUnifiedLoginTokenFailuresRequireReconnect(t *testing.T) {
	for name, err := range map[string]error{
		"meta rejects the token": &platform.HTTPError{StatusCode: 401, Code: "190"},
		"forced refresh has no flow": fmt.Errorf("instagram:unified_login token refresh failed after expiry: %w",
			errors.New("token refresh is not supported for platform instagram")),
		"the stored expiry passed": errors.New("token expired for account a1 and provider does not support refresh"),
	} {
		t.Run(name, func(t *testing.T) {
			failure := ClassifyFailure(err)
			require.Equal(t, FailureActionReconnect, failure.Action, "kind %s", failure.Kind)
			require.False(t, failure.Retryable)
		})
	}
}
