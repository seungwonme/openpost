package platform

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// The request shapes below come from Meta's "Set up Instagram Webhooks" guide,
// not from the implementation: one edge per login mode, token in the header.
func TestInstagramWebhookSubscriptionRequestShapePerConnection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		adapter *InstagramAdapter
		input   InstagramWebhookSubscription
		host    string
		path    string
	}{
		{
			name:    "instagram login posts on the Instagram account",
			adapter: NewInstagramLoginAdapter("app", "secret", "https://app.example/callback"),
			input:   InstagramWebhookSubscription{AccessToken: "ig-user-token", AccountID: "17841400000000123", PageID: "999"},
			host:    "graph.instagram.com",
			path:    "/" + metaGraphAPIVersion() + "/17841400000000123/subscribed_apps",
		},
		{
			name:    "facebook login posts on the linked Page",
			adapter: NewInstagramAdapter("app", "secret", "https://app.example/callback"),
			input:   InstagramWebhookSubscription{AccessToken: "page-token", AccountID: "17841400000000123", PageID: "104200000000456"},
			host:    "graph.facebook.com",
			path:    "/" + metaGraphAPIVersion() + "/104200000000456/subscribed_apps",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := httpClient
			t.Cleanup(func() { httpClient = original })
			calls := 0
			httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodPost, req.Method)
				require.Equal(t, tc.host, req.URL.Host)
				require.Equal(t, tc.path, req.URL.Path)
				require.Equal(t, "comments,messages", req.URL.Query().Get("subscribed_fields"))
				require.Equal(t, "Bearer "+tc.input.AccessToken, req.Header.Get("Authorization"))
				require.Empty(t, req.URL.Query().Get("access_token"), "tokens stay out of URLs and logs")
				return jsonResponse(req, `{"success":true}`), nil
			})}
			require.NoError(t, tc.adapter.SubscribeInstagramWebhooks(t.Context(), tc.input))
			require.Equal(t, 1, calls)
		})
	}
}

func TestInstagramWebhookSubscriptionRefusesWhatItCannotAddress(t *testing.T) {
	original := httpClient
	t.Cleanup(func() { httpClient = original })
	httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("unaddressable subscription must not reach Meta: %s", req.URL)
		return nil, nil
	})}
	facebook := NewInstagramAdapter("app", "secret", "https://app.example/callback")
	for name, input := range map[string]InstagramWebhookSubscription{
		"facebook login without a Page": {AccessToken: "t", AccountID: "17841400000000123"},
		"page id with a path":           {AccessToken: "t", AccountID: "1", PageID: "1/../me"},
		"missing token":                 {AccountID: "1", PageID: "2"},
	} {
		t.Run(name, func(t *testing.T) {
			err := facebook.SubscribeInstagramWebhooks(t.Context(), input)
			require.ErrorIs(t, err, ErrInstagramWebhookUnsupported)
			require.Equal(t, InstagramWebhookUnsupportedConnection, ClassifyInstagramWebhookError(err).Code)
		})
	}
	login := NewInstagramLoginAdapter("app", "secret", "https://app.example/callback")
	require.ErrorIs(t, login.SubscribeInstagramWebhooks(t.Context(), InstagramWebhookSubscription{AccessToken: "t", AccountID: "not-numeric"}), ErrInstagramWebhookUnsupported)
}

func TestInstagramWebhookSubscriptionRequiresConfirmation(t *testing.T) {
	original := httpClient
	t.Cleanup(func() { httpClient = original })
	httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, `{"success":false}`), nil
	})}
	err := NewInstagramLoginAdapter("app", "secret", "https://app.example/callback").
		SubscribeInstagramWebhooks(context.Background(), InstagramWebhookSubscription{AccessToken: "t", AccountID: "1"})
	require.Error(t, err)
}

func TestClassifyInstagramWebhookError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{"expired token", &HTTPError{StatusCode: 400, Code: "190"}, InstagramWebhookAuthRequired},
		{"unauthorized", &HTTPError{StatusCode: 401}, InstagramWebhookAuthRequired},
		{"missing permission", &HTTPError{StatusCode: 403, Code: "10"}, InstagramWebhookPermissionDenied},
		{"app permission", &HTTPError{StatusCode: 400, Code: "200"}, InstagramWebhookPermissionDenied},
		{"app throttled", &HTTPError{StatusCode: 400, Code: "4"}, InstagramWebhookRateLimited},
		{"http throttled", &HTTPError{StatusCode: 429}, InstagramWebhookRateLimited},
		{"meta outage", &HTTPError{StatusCode: 503}, InstagramWebhookUnavailable},
		{"network", fmt.Errorf("dial: %w", context.DeadlineExceeded), InstagramWebhookUnavailable},
		{"invalid parameter", &HTTPError{StatusCode: 400, Code: "100"}, InstagramWebhookRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := ClassifyInstagramWebhookError(tc.err)
			require.Equal(t, tc.code, failure.Code)
			require.NotEmpty(t, failure.Message)
		})
	}
}
