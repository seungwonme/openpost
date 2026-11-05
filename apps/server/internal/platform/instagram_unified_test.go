package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The contracts below come from Meta's "Instagram API with Unified Login"
// guides, not from the implementation: code exchange on graph.facebook.com,
// /me/assigned_instagram_accounts for discovery, and the Instagram account ID
// in every path. A real HTTP server records what each flow sends.

const (
	unifiedAccountID = "17841400000000123"
	unifiedToken     = "system-user-token"
)

type graphRequest struct {
	Host, Method, Path, Authorization string
	Query                             url.Values
	Body                              string
}

type fakeGraph struct {
	mu       sync.Mutex
	requests []graphRequest
}

func (g *fakeGraph) record(req graphRequest) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, req)
}

func (g *fakeGraph) paths() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	paths := make([]string, 0, len(g.requests))
	for _, req := range g.requests {
		paths = append(paths, req.Path)
	}
	return paths
}

func (g *fakeGraph) all() []graphRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]graphRequest(nil), g.requests...)
}

// startFakeGraph serves every Meta host from one httptest server, keeping the
// host the adapter chose. Answers are keyed by "METHOD /path".
func startFakeGraph(t *testing.T, responses map[string]string) *fakeGraph {
	t.Helper()
	t.Setenv("META_GRAPH_API_VERSION", "v25.0")
	graph := &fakeGraph{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		graph.record(graphRequest{
			Host: r.Header.Get("X-Original-Host"), Method: r.Method, Path: r.URL.Path,
			Authorization: r.Header.Get("Authorization"), Query: r.URL.Query(), Body: string(body),
		})
		w.Header().Set("Content-Type", "application/json")
		response, ok := responses[r.Method+" "+r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, fmt.Sprintf(`{"error":{"message":"unexpected %s %s","code":803}}`, r.Method, r.URL.Path))
			return
		}
		if status, payload, found := strings.Cut(response, "|"); found && len(status) == 3 {
			code := 0
			_, _ = fmt.Sscanf(status, "%d", &code)
			w.WriteHeader(code)
			response = payload
		}
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	original := httpClient
	t.Cleanup(func() { httpClient = original })
	httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		forwarded := req.Clone(req.Context())
		forwarded.Header.Set("X-Original-Host", req.URL.Host)
		forwarded.URL.Scheme = target.Scheme
		forwarded.URL.Host = target.Host
		forwarded.Host = target.Host
		return http.DefaultTransport.RoundTrip(forwarded)
	})}
	return graph
}

func configuredInstagramAdapter() *InstagramAdapter {
	adapter := NewInstagramAdapter("meta-app", "meta-secret", "https://app.example/callback")
	adapter.configID = "config-123"
	return adapter
}

func debugTokenBody(tokenType string, expiresAt int64, scopes ...string) string {
	encoded, _ := json.Marshal(scopes)
	typeField := ""
	if tokenType != "" {
		typeField = fmt.Sprintf(`"type":%q,`, tokenType)
	}
	return fmt.Sprintf(`{"data":{%s"app_id":"meta-app","is_valid":true,"expires_at":%d,"user_id":"system-user-1","scopes":%s}}`, typeField, expiresAt, encoded)
}

var unifiedScopes = []string{"instagram_basic", "instagram_content_publish", "instagram_manage_comments", "instagram_manage_messages", "instagram_manage_insights"}

func TestInstagramConfigurationExchangeDetectsTokenTypeFromEvidence(t *testing.T) {
	const (
		exchange   = "POST /v25.0/oauth/access_token"
		debugToken = "GET /v25.0/debug_token"
		me         = "GET /v25.0/me"
	)
	shortToken := `{"access_token":"exchanged-token","token_type":"bearer"}`

	t.Run("debug_token type marks a system user token", func(t *testing.T) {
		graph := startFakeGraph(t, map[string]string{
			exchange:   shortToken,
			debugToken: debugTokenBody("SYSTEM_USER", 0, unifiedScopes...),
		})
		token, err := configuredInstagramAdapter().ExchangeCode(t.Context(), "code", nil)
		require.NoError(t, err)
		require.Equal(t, ConnectionModeUnifiedLogin, token.Extra["connection_type"])
		require.Equal(t, strings.Join(unifiedScopes, " "), token.Extra["scope"])
		require.Equal(t, "system-user-1", token.Extra["user_id"])
		require.Zero(t, token.ExpiresIn, "a non-expiring token has no expiry to refresh")
		require.Equal(t, []string{"/v25.0/oauth/access_token", "/v25.0/debug_token"}, graph.paths(),
			"no long-lived exchange and no /me/permissions for a system user token")
		inspection := graph.all()[1]
		require.Equal(t, "exchanged-token", inspection.Query.Get("input_token"))
		require.Equal(t, "Bearer meta-app|meta-secret", inspection.Authorization)
		require.Empty(t, inspection.Query.Get("access_token"), "the app token stays out of the URL")
	})

	t.Run("client_business_id marks a system user token when the type is absent", func(t *testing.T) {
		graph := startFakeGraph(t, map[string]string{
			exchange:   shortToken,
			debugToken: debugTokenBody("", 0, unifiedScopes...),
			me:         `{"id":"system-user-1","client_business_id":"business-1"}`,
		})
		token, err := configuredInstagramAdapter().ExchangeCode(t.Context(), "code", nil)
		require.NoError(t, err)
		require.Equal(t, ConnectionModeUnifiedLogin, token.Extra["connection_type"])
		require.NotContains(t, token.Extra, "client_business_id", "OpenPost has no refresh flow that needs it")
		require.Equal(t, "id,client_business_id", graph.all()[2].Query.Get("fields"))
	})

	for name, responses := range map[string]map[string]string{
		"debug_token says USER": {
			debugToken: debugTokenBody("USER", 0, "instagram_basic"),
		},
		"no type and /me has no client business": {
			debugToken: debugTokenBody("", 0, "instagram_basic"),
			me:         `{"id":"person-1"}`,
		},
		"no type and /me has no such field": {
			debugToken: debugTokenBody("", 0, "instagram_basic"),
			me:         `400|{"error":{"message":"(#100) Tried accessing nonexisting field","code":100}}`,
		},
	} {
		t.Run("a user token keeps the Page flow: "+name, func(t *testing.T) {
			responses[exchange] = shortToken
			responses["GET /v25.0/me/permissions"] = `{"data":[{"permission":"instagram_basic","status":"granted"},{"permission":"pages_show_list","status":"declined"}]}`
			graph := startFakeGraph(t, responses)
			token, err := configuredInstagramAdapter().ExchangeCode(t.Context(), "code", nil)
			require.NoError(t, err)
			require.NotContains(t, token.Extra, "connection_type")
			require.Equal(t, "instagram_basic", token.Extra["scope"])
			exchanges := 0
			for _, path := range graph.paths() {
				if path == "/v25.0/oauth/access_token" {
					exchanges++
				}
			}
			require.Equal(t, 2, exchanges, "a user token still attempts the long-lived exchange")
		})
	}

	for name, responses := range map[string]map[string]string{
		"debug_token is unavailable": {
			debugToken: `500|{"error":{"message":"down","code":1}}`,
		},
		"the inspected token is invalid": {
			debugToken: `{"data":{"is_valid":false,"scopes":["instagram_basic"]}}`,
		},
		"no type and /me is unavailable": {
			debugToken: debugTokenBody("", 0, "instagram_basic"),
			me:         `500|{"error":{"message":"down","code":1}}`,
		},
		"no type and /me rejects the token": {
			debugToken: debugTokenBody("", 0, "instagram_basic"),
			me:         `401|{"error":{"message":"bad","code":190}}`,
		},
		"a system user token reports no permissions": {
			debugToken: debugTokenBody("SYSTEM_USER", 0),
		},
		"a system user token has already expired": {
			debugToken: debugTokenBody("SYSTEM_USER", time.Now().Add(-time.Hour).Unix(), "instagram_basic"),
		},
	} {
		t.Run("the connection stops without guessing: "+name, func(t *testing.T) {
			responses[exchange] = shortToken
			graph := startFakeGraph(t, responses)
			_, err := configuredInstagramAdapter().ExchangeCode(t.Context(), "code", nil)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "exchanged-token")
			require.NotContains(t, err.Error(), "meta-secret")
			require.NotContains(t, graph.paths(), "/v25.0/me/permissions", "no scope is assumed from another source")
		})
	}

	t.Run("a numeric user_id does not fail the connection", func(t *testing.T) {
		startFakeGraph(t, map[string]string{
			exchange:   shortToken,
			debugToken: `{"data":{"type":"SYSTEM_USER","is_valid":true,"expires_at":0,"user_id":118,"scopes":["instagram_basic"]}}`,
		})
		token, err := configuredInstagramAdapter().ExchangeCode(t.Context(), "code", nil)
		require.NoError(t, err)
		require.Equal(t, "118", token.Extra["user_id"])
	})

	t.Run("an expiring configuration keeps its real expiry", func(t *testing.T) {
		startFakeGraph(t, map[string]string{
			exchange:   shortToken,
			debugToken: debugTokenBody("SYSTEM_USER", time.Now().Add(60*24*time.Hour).Unix(), unifiedScopes...),
		})
		token, err := configuredInstagramAdapter().ExchangeCode(t.Context(), "code", nil)
		require.NoError(t, err)
		require.InDelta(t, (60 * 24 * time.Hour).Seconds(), float64(token.ExpiresIn), 60)
	})

	t.Run("an app without config_id never inspects the token", func(t *testing.T) {
		graph := startFakeGraph(t, map[string]string{
			exchange:                    shortToken,
			"GET /v25.0/me/permissions": `{"data":[{"permission":"instagram_basic","status":"granted"}]}`,
		})
		adapter := NewInstagramAdapter("meta-app", "meta-secret", "https://app.example/callback")
		token, err := adapter.ExchangeCode(t.Context(), "code", nil)
		require.NoError(t, err)
		require.NotContains(t, token.Extra, "connection_type")
		require.NotContains(t, graph.paths(), "/v25.0/debug_token")
	})
}

func unifiedTokenResult() *TokenResult {
	return &TokenResult{AccessToken: unifiedToken, ExpiresIn: 3600, Extra: map[string]string{
		"connection_type": ConnectionModeUnifiedLogin, "scope": strings.Join(unifiedScopes, " "), "_grant_subject": "system-user-1",
	}}
}

func TestInstagramUnifiedDiscoveryUsesAssignedAccountsAndNoPage(t *testing.T) {
	graph := startFakeGraph(t, map[string]string{
		"GET /v25.0/me/assigned_instagram_accounts": `{"data":[{"id":"` + unifiedAccountID + `","username":"studio"},{"id":"","username":"blank"},{"id":"17841400000000456"}]}`,
	})
	options, err := configuredInstagramAdapter().ListAccountSelections(t.Context(), unifiedTokenResult())
	require.NoError(t, err)
	require.Len(t, options, 2)
	require.Equal(t, unifiedAccountID, options[0].ID)
	require.Equal(t, "studio", options[0].Username)
	require.Equal(t, "17841400000000456", options[1].Username, "an account without a username stays identifiable")
	require.NotContains(t, options[0].Extra, "page_id")
	requests := graph.all()
	require.Len(t, requests, 1, "/me/accounts is never called for a system user token")
	require.Equal(t, "graph.facebook.com", requests[0].Host)
	require.Equal(t, "id,username", requests[0].Query.Get("fields"))
	require.Equal(t, "Bearer "+unifiedToken, requests[0].Authorization)
	require.Empty(t, requests[0].Query.Get("access_token"))

	t.Run("no shared account has its own error", func(t *testing.T) {
		startFakeGraph(t, map[string]string{"GET /v25.0/me/assigned_instagram_accounts": `{"data":[]}`})
		_, err := configuredInstagramAdapter().ListAccountSelections(t.Context(), unifiedTokenResult())
		require.ErrorIs(t, err, ErrNoInstagramAssignedAccount)
		require.NotErrorIs(t, err, ErrNoInstagramProfessionalAccount)
	})

	t.Run("a user token still lists Pages", func(t *testing.T) {
		graph := startFakeGraph(t, map[string]string{
			"GET /v25.0/me/accounts": `{"data":[{"id":"page-1","name":"Page","access_token":"page-token","instagram_business_account":{"id":"` + unifiedAccountID + `","username":"studio"}}]}`,
		})
		options, err := configuredInstagramAdapter().ListAccountSelections(t.Context(), &TokenResult{AccessToken: "user-token"})
		require.NoError(t, err)
		require.Equal(t, "page-1", options[0].Extra["page_id"])
		require.Equal(t, []string{"/v25.0/me/accounts"}, graph.paths())
	})
}

func TestInstagramUnifiedSelectionStoresNoPage(t *testing.T) {
	assigned := `{"data":[{"id":"` + unifiedAccountID + `","username":"studio"}]}`
	t.Run("the shared token and connection type are recorded", func(t *testing.T) {
		startFakeGraph(t, map[string]string{
			"GET /v25.0/me/assigned_instagram_accounts": assigned,
			"GET /v25.0/" + unifiedAccountID:            `{"id":"` + unifiedAccountID + `","username":"studio_live","name":"Studio","profile_picture_url":"https://cdn.example/a.jpg"}`,
		})
		token := unifiedTokenResult()
		selected, err := configuredInstagramAdapter().SelectAccount(t.Context(), token, unifiedAccountID)
		require.NoError(t, err)
		require.Equal(t, unifiedAccountID, selected.AccountID)
		require.Equal(t, "studio_live", selected.AccountUsername)
		require.Equal(t, "https://cdn.example/a.jpg", selected.AccountAvatarURL)
		require.Equal(t, map[string]string{"connection_type": "unified_login", "instagram_account_type": "professional"}, selected.CapabilityState)
		require.Equal(t, unifiedToken, selected.Token.AccessToken, "one token covers the shared accounts")
		require.Equal(t, 3600, selected.Token.ExpiresIn, "the configured expiry survives selection")
		require.NotContains(t, selected.Token.Extra, "page_id")
		require.Equal(t, unifiedAccountID, selected.Token.Extra["instagram_business_account_id"])
		require.NotContains(t, token.Extra, "instagram_business_account_id", "the pending token is not mutated")
	})

	t.Run("a missing picture does not fail the connection", func(t *testing.T) {
		startFakeGraph(t, map[string]string{"GET /v25.0/me/assigned_instagram_accounts": assigned})
		selected, err := configuredInstagramAdapter().SelectAccount(t.Context(), unifiedTokenResult(), unifiedAccountID)
		require.NoError(t, err)
		require.Equal(t, "studio", selected.AccountUsername)
		require.Empty(t, selected.AccountAvatarURL)
	})

	t.Run("an account the business did not share is rejected", func(t *testing.T) {
		startFakeGraph(t, map[string]string{"GET /v25.0/me/assigned_instagram_accounts": assigned})
		_, err := configuredInstagramAdapter().SelectAccount(t.Context(), unifiedTokenResult(), "17841499999999999")
		require.ErrorContains(t, err, "was not found")
	})
}

func TestInstagramUnifiedRegistryAndAccountKey(t *testing.T) {
	require.Equal(t, InstagramUnifiedProviderKey, AccountProviderKey("instagram", "", `{"connection_type":"unified_login"}`))
	require.Equal(t, "instagram:unified_login", InstagramUnifiedProviderKey)
	require.Equal(t, "instagram:instagram_login", AccountProviderKey("instagram", "", `{"connection_type":"instagram_login"}`))
	require.Equal(t, "instagram", AccountProviderKey("instagram", "", `{"facebook_page_id":"1"}`))
	require.Equal(t, "instagram", AccountProviderKey("instagram", "", `{}`))
	require.Equal(t, "instagram", AccountProviderKey("instagram", "", ``))

	adapters, _, err := BuildAdapterRegistry([]AppConfig{{
		Provider: "instagram", ClientID: "meta-app", ClientSecret: "meta-secret", RedirectURI: "https://app.test/callback", ConfigID: "config-123",
	}}, RegistryOptions{})
	require.NoError(t, err)
	unified, ok := adapters[InstagramUnifiedProviderKey].(*InstagramAdapter)
	require.True(t, ok)
	require.True(t, unified.unifiedLogin)
	require.Equal(t, "config-123", unified.configID, "a reconnect through this adapter must keep the configuration dialog")
	require.False(t, unified.instagramLogin)
	plain, ok := adapters["instagram"].(*InstagramAdapter)
	require.True(t, ok)
	require.False(t, plain.unifiedLogin, "user-token accounts keep the Facebook Login adapter")
	require.Equal(t, "config-123", plain.configID)
	require.Equal(t, "instagram", AppConfigMergeKey(AppConfig{Provider: "instagram", ClientID: "meta-app", ConfigID: "config-123"}),
		"the callback ID stays instagram:<client_id>")

	login, _, err := BuildAdapterRegistry([]AppConfig{{
		Provider: "instagram", ConnectionMode: ConnectionModeInstagramLogin, ClientID: "ig-app", ClientSecret: "ig-secret", RedirectURI: "https://app.test/callback",
	}}, RegistryOptions{})
	require.NoError(t, err)
	require.NotContains(t, login, InstagramUnifiedProviderKey, "a direct-login app cannot serve a Meta app token")
}

// Every API path must address the Instagram account or its objects on
// graph.facebook.com, with no Page lookup and no graph.instagram.com request.
func TestInstagramUnifiedAPIPathsNeedNoPage(t *testing.T) {
	adapter := NewInstagramUnifiedLoginAdapter("meta-app", "meta-secret", "https://app.example/callback")
	ctx := context.Background()
	base := "/v25.0/" + unifiedAccountID
	for _, tc := range []struct {
		name      string
		responses map[string]string
		call      func() error
		want      []string
	}{
		{
			name: "publish a single image",
			responses: map[string]string{
				"POST " + base + "/media":         `{"id":"900"}`,
				"GET /v25.0/900":                  `{"status_code":"FINISHED"}`,
				"POST " + base + "/media_publish": `{"id":"901"}`,
			},
			call: func() error {
				_, err := adapter.Publish(ctx, unifiedToken, unifiedAccountID, &PublishRequest{
					Content: "Caption", PlatformMediaIDs: []string{"https://media.example/a.jpg"}, Media: []MediaItem{{MimeType: "image/jpeg"}},
				})
				return err
			},
			want: []string{"POST " + base + "/media", "GET /v25.0/900", "POST " + base + "/media_publish"},
		},
		{
			name:      "first comment",
			responses: map[string]string{"POST /v25.0/901/comments": `{"id":"c1"}`},
			call: func() error {
				_, err := adapter.PostFirstComment(ctx, unifiedToken, unifiedAccountID, "901", "hello")
				return err
			},
			want: []string{"POST /v25.0/901/comments"},
		},
		{
			name:      "list comments",
			responses: map[string]string{"GET /v25.0/901/comments": `{"data":[]}`},
			call: func() error {
				_, err := adapter.ListComments(ctx, unifiedToken, unifiedAccountID, "901")
				return err
			},
			want: []string{"GET /v25.0/901/comments"},
		},
		{
			name:      "reply to a comment",
			responses: map[string]string{"POST /v25.0/c1/replies": `{"id":"r1"}`},
			call: func() error {
				_, err := adapter.ReplyToComment(ctx, unifiedToken, unifiedAccountID, "c1", "thanks")
				return err
			},
			want: []string{"POST /v25.0/c1/replies"},
		},
		{
			name:      "hide a comment",
			responses: map[string]string{"POST /v25.0/c1": `{"success":true}`},
			call:      func() error { return adapter.HideComment(ctx, unifiedToken, unifiedAccountID, "c1") },
			want:      []string{"POST /v25.0/c1"},
		},
		{
			name:      "delete a comment",
			responses: map[string]string{"DELETE /v25.0/c1": `{"success":true}`},
			call:      func() error { return adapter.DeleteComment(ctx, unifiedToken, unifiedAccountID, "c1") },
			want:      []string{"DELETE /v25.0/c1"},
		},
		{
			name:      "private reply",
			responses: map[string]string{"POST " + base + "/messages": `{"message_id":"m1"}`},
			call: func() error {
				_, err := adapter.ReplyPrivatelyToComment(ctx, unifiedToken, unifiedAccountID, "c1", "details")
				return err
			},
			want: []string{"POST " + base + "/messages"},
		},
		{
			name:      "read conversations",
			responses: map[string]string{"GET " + base + "/conversations": `{"data":[]}`},
			call: func() error {
				_, err := adapter.FetchMessages(ctx, unifiedToken, FetchMessagesRequest{AccountID: unifiedAccountID})
				return err
			},
			want: []string{"GET " + base + "/conversations"},
		},
		{
			name:      "send a message",
			responses: map[string]string{"POST " + base + "/messages": `{"message_id":"m2"}`},
			call: func() error {
				_, err := adapter.SendMessage(ctx, unifiedToken, SendMessageRequest{AccountID: unifiedAccountID, CounterpartRemoteID: "reader", Body: "hi"})
				return err
			},
			want: []string{"POST " + base + "/messages"},
		},
		{
			name:      "read message history",
			responses: map[string]string{"GET /v25.0/conv1/messages": `{"data":[]}`},
			call: func() error {
				_, err := adapter.FetchMessageHistory(ctx, unifiedToken, FetchMessageHistoryRequest{AccountID: unifiedAccountID, ConversationID: "conv1"})
				return err
			},
			want: []string{"GET /v25.0/conv1/messages"},
		},
		{
			name:      "resolve a message webhook",
			responses: map[string]string{"GET " + base + "/conversations": `{"data":[{"id":"conv1","participants":{"data":[{"id":"reader","username":"reader"}]}}]}`},
			call: func() error {
				_, err := adapter.NormalizeMessagingWebhook(ctx, unifiedToken, unifiedAccountID,
					json.RawMessage(`{"sender":{"id":"reader"},"recipient":{"id":"`+unifiedAccountID+`"},"timestamp":1792490400000,"message":{"mid":"m3","text":"hi"}}`))
				return err
			},
			want: []string{"GET " + base + "/conversations"},
		},
		{
			name:      "refresh account metadata",
			responses: map[string]string{"GET " + base: `{"id":"` + unifiedAccountID + `","username":"studio"}`},
			call: func() error {
				profile, err := adapter.RefreshAccountMetadata(ctx, unifiedToken, AccountMetadataRequest{AccountID: unifiedAccountID})
				if err == nil && profile.ID != unifiedAccountID {
					return fmt.Errorf("unexpected profile %q", profile.ID)
				}
				return err
			},
			want: []string{"GET " + base},
		},
		{
			name:      "account analytics",
			responses: map[string]string{"GET " + base: `{"followers_count":10,"media_count":2}`},
			call: func() error {
				_, err := adapter.FetchAccountAnalytics(ctx, unifiedToken, AccountAnalyticsRequest{AccountID: unifiedAccountID})
				return err
			},
			want: []string{"GET " + base},
		},
		{
			name: "content analytics",
			responses: map[string]string{
				"GET /v25.0/901":          `{"like_count":3,"comments_count":1}`,
				"GET /v25.0/901/insights": `{"data":[]}`,
			},
			call: func() error {
				_, err := adapter.FetchContentAnalytics(ctx, unifiedToken, ContentAnalyticsRequest{AccountID: unifiedAccountID, ExternalIDs: []string{"901"}, Profile: "image_post"})
				return err
			},
			want: []string{"GET /v25.0/901", "GET /v25.0/901/insights"},
		},
		{
			name:      "post import",
			responses: map[string]string{"GET " + base + "/media": `{"data":[]}`},
			call: func() error {
				_, err := adapter.ListNativePosts(ctx, unifiedToken, NativePostRequest{AccountID: unifiedAccountID})
				return err
			},
			want: []string{"GET " + base + "/media"},
		},
		{
			name:      "subscribe to webhooks on the Instagram account edge",
			responses: map[string]string{"POST " + base + "/subscribed_apps": `{"success":true}`},
			call: func() error {
				return adapter.SubscribeInstagramWebhooks(ctx, InstagramWebhookSubscription{AccessToken: unifiedToken, AccountID: unifiedAccountID})
			},
			want: []string{"POST " + base + "/subscribed_apps"},
		},
		{
			name:      "preflight reads the system user",
			responses: map[string]string{"GET /v25.0/me": `{"id":"system-user-1","name":"Integration"}`},
			call: func() error {
				profile, err := adapter.GetProfile(ctx, unifiedToken)
				if err == nil && profile.ID != "system-user-1" {
					return fmt.Errorf("unexpected profile %q", profile.ID)
				}
				return err
			},
			want: []string{"GET /v25.0/me"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			graph := startFakeGraph(t, tc.responses)
			require.NoError(t, tc.call())
			requests := graph.all()
			got := make([]string, 0, len(requests))
			for _, req := range requests {
				got = append(got, req.Method+" "+req.Path)
				require.Equal(t, "graph.facebook.com", req.Host)
				require.NotContains(t, req.Path, "/accounts", "no Page discovery")
				carried := strings.Contains(req.Authorization, unifiedToken) || strings.Contains(req.Body, unifiedToken) || req.Query.Get("access_token") == unifiedToken
				require.True(t, carried, "the system user token authorizes %s %s", req.Method, req.Path)
			}
			require.Subset(t, got, tc.want)
		})
	}

	t.Run("webhook subscription sends the fields in the query with a bearer token", func(t *testing.T) {
		graph := startFakeGraph(t, map[string]string{"POST " + base + "/subscribed_apps": `{"success":true}`})
		require.NoError(t, adapter.SubscribeInstagramWebhooks(ctx, InstagramWebhookSubscription{AccessToken: unifiedToken, AccountID: unifiedAccountID, PageID: "must-be-ignored"}))
		request := graph.all()[0]
		require.Equal(t, "comments,messages", request.Query.Get("subscribed_fields"))
		require.Equal(t, "Bearer "+unifiedToken, request.Authorization)
		require.Empty(t, request.Query.Get("access_token"))
	})

	t.Run("webhook subscription without an account is unsupported", func(t *testing.T) {
		startFakeGraph(t, nil)
		err := adapter.SubscribeInstagramWebhooks(ctx, InstagramWebhookSubscription{AccessToken: unifiedToken})
		require.ErrorIs(t, err, ErrInstagramWebhookUnsupported)
	})

	t.Run("private replies address the comment, not the commenter", func(t *testing.T) {
		graph := startFakeGraph(t, map[string]string{"POST " + base + "/messages": `{"message_id":"m1"}`})
		_, err := adapter.ReplyPrivatelyToComment(ctx, unifiedToken, unifiedAccountID, "c1", "details")
		require.NoError(t, err)
		require.JSONEq(t, `{"recipient":{"comment_id":"c1"},"message":{"text":"details"}}`, graph.all()[0].Body)
	})

	t.Run("conversations ask for Instagram threads", func(t *testing.T) {
		graph := startFakeGraph(t, map[string]string{"GET " + base + "/conversations": `{"data":[]}`})
		_, err := adapter.FetchMessages(ctx, unifiedToken, FetchMessagesRequest{AccountID: unifiedAccountID})
		require.NoError(t, err)
		require.Equal(t, "instagram", graph.all()[0].Query.Get("platform"))
	})
}

func TestInstagramUnifiedHasNoDocumentedLocationSearch(t *testing.T) {
	adapter := NewInstagramUnifiedLoginAdapter("meta-app", "meta-secret", "https://app.example/callback")
	graph := startFakeGraph(t, nil)

	_, err := adapter.SearchPublishingOptions(t.Context(), unifiedToken, PublishingOptionsInput{Source: "instagram_locations", Search: "Seoul"})
	require.ErrorContains(t, err, "requires a Facebook Login connection with a linked Page")

	_, err = adapter.Publish(t.Context(), unifiedToken, unifiedAccountID, &PublishRequest{
		Content: "Caption", PlatformMediaIDs: []string{"https://media.example/a.jpg"}, Media: []MediaItem{{MimeType: "image/jpeg"}},
		Settings: map[string]interface{}{"location_id": "123"},
	})
	require.ErrorContains(t, err, "location_id requires a Facebook Login connection")
	require.Empty(t, graph.all(), "a blocked setting never reaches Meta")

	require.NoError(t, adapter.validateLoginSettings(&PublishRequest{
		MediaSettings: []map[string]interface{}{{"product_tags": `[{"product_id":"1"}]`}},
	}), "Meta lists product tagging as supported for Unified Login")
}

func TestInstagramSupportScopesPerConnection(t *testing.T) {
	unified := NewInstagramUnifiedLoginAdapter("a", "s", "r")
	facebook := NewInstagramAdapter("a", "s", "r")
	direct := NewInstagramLoginAdapter("a", "s", "r")

	require.Equal(t, []string{"instagram_manage_comments"}, unified.EngagementSupport().RequiredScopes)
	require.Equal(t, []string{"instagram_basic", "instagram_manage_messages"}, unified.MessagingSupport().RequiredScopes)
	require.Equal(t, []string{"instagram_manage_insights"}, unified.AnalyticsSupport().ContentRequiredScopes)
	require.Equal(t, []string{"instagram_basic"}, unified.NativePostSupport().RequiredScopes)

	require.Equal(t, []string{"instagram_manage_comments", "pages_read_engagement"}, facebook.EngagementSupport().RequiredScopes)
	require.Equal(t, []string{"instagram_basic", "instagram_manage_messages", "pages_manage_metadata"}, facebook.MessagingSupport().RequiredScopes)
	require.Equal(t, []string{"instagram_business_manage_comments"}, direct.EngagementSupport().RequiredScopes)
	require.Equal(t, []string{"instagram_business_basic", "instagram_business_manage_messages"}, direct.MessagingSupport().RequiredScopes)

	require.Equal(t, []string{"instagram_basic", "instagram_manage_comments", "instagram_manage_messages"}, InstagramPrivateReplyScopes(InstagramUnifiedProviderKey))
	require.Equal(t, []string{"instagram_manage_comments"}, InstagramPrivateReplyScopes("instagram"))
	require.Equal(t, []string{"instagram_business_manage_comments"}, InstagramPrivateReplyScopes("instagram:instagram_login"))

	for _, adapter := range []*InstagramAdapter{unified, facebook} {
		require.False(t, adapter.RefreshCapability().Supported, "OpenPost implements no refresh flow for these tokens")
	}
	require.True(t, direct.RefreshCapability().Supported)
}
