package instagramwebhook

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openpost/backend/internal/platform"
)

func TestUnifiedLoginAccountSubscribesThroughItsOwnAdapterWithNoPage(t *testing.T) {
	db := testDB(t)
	seedAccount(t, db, "ig1", "instagram", `{"connection_type":"unified_login","instagram_account_type":"professional"}`)
	unified, facebook := &fakeInstagram{}, &fakeInstagram{}
	svc := NewService(db, &tokenSource{token: "system-user-token"}, map[string]platform.Adapter{
		"instagram": facebook, platform.InstagramUnifiedProviderKey: unified,
	})

	require.NoError(t, svc.Ensure(t.Context(), "ig1"))
	require.Equal(t, []platform.InstagramWebhookSubscription{{AccessToken: "system-user-token", AccountID: "17841400000000123"}}, unified.inputs)
	require.Empty(t, facebook.inputs, "a system user token must not be used on the Page path")
	require.Empty(t, readiness(t, db))
}

func TestUnifiedLoginSubscriptionFailureAsksForReconnection(t *testing.T) {
	db := testDB(t)
	seedAccount(t, db, "ig1", "instagram", `{"connection_type":"unified_login"}`)
	adapter := &fakeInstagram{errs: []error{&platform.HTTPError{StatusCode: 400, Code: "190"}}}
	svc := NewService(db, &tokenSource{token: "system-user-token"}, map[string]platform.Adapter{platform.InstagramUnifiedProviderKey: adapter})

	require.Error(t, svc.Ensure(t.Context(), "ig1"))
	failure := platform.ClassifyInstagramWebhookError(&platform.HTTPError{StatusCode: 400, Code: "190"})
	require.Equal(t, platform.InstagramWebhookAuthRequired, failure.Code)
	require.Contains(t, failure.Message, "Reconnect")
	require.Len(t, adapter.inputs, 1, "a rejected token is not retried in the background")
}
