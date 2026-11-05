package platform

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	configExchangePath   = "POST /v25.0/oauth/access_token"
	configDebugTokenPath = "GET /v25.0/debug_token"
	configMePath         = "GET /v25.0/me"
)

// A token whose inspection says it never expires must not inherit an expiry
// from the code exchange: nothing can refresh it, so it would falsely expire.
func TestInstagramUnifiedExpiryFollowsTheInspection(t *testing.T) {
	longExchange := `{"access_token":"exchanged-token","token_type":"bearer","expires_in":5184000}`

	t.Run("expires_at 0 is stored as non-expiring despite expires_in", func(t *testing.T) {
		startFakeGraph(t, map[string]string{
			configExchangePath:   longExchange,
			configDebugTokenPath: debugTokenBody("SYSTEM_USER", 0, unifiedScopes...),
		})
		token, err := configuredInstagramAdapter().ExchangeCode(t.Context(), "code", nil)
		require.NoError(t, err)
		require.Zero(t, token.ExpiresIn)
	})

	t.Run("an inspection without expires_at keeps the exchange expiry", func(t *testing.T) {
		startFakeGraph(t, map[string]string{
			configExchangePath:   longExchange,
			configDebugTokenPath: `{"data":{"type":"SYSTEM_USER","is_valid":true,"scopes":["instagram_basic"]}}`,
		})
		token, err := configuredInstagramAdapter().ExchangeCode(t.Context(), "code", nil)
		require.NoError(t, err)
		require.Equal(t, 5184000, token.ExpiresIn, "absent is not a statement that the token never expires")
	})

	t.Run("a future expires_at wins over expires_in", func(t *testing.T) {
		startFakeGraph(t, map[string]string{
			configExchangePath:   longExchange,
			configDebugTokenPath: debugTokenBody("SYSTEM_USER", time.Now().Add(time.Hour).Unix(), unifiedScopes...),
		})
		token, err := configuredInstagramAdapter().ExchangeCode(t.Context(), "code", nil)
		require.NoError(t, err)
		require.InDelta(t, time.Hour.Seconds(), float64(token.ExpiresIn), 60)
	})
}

type debugTokenStep func(*http.Request) (*http.Response, error)

// scriptDebugToken answers each debug_token call from steps and counts calls.
// The authorization code is single-use, so its exchange must never repeat.
func scriptDebugToken(t *testing.T, steps ...debugTokenStep) (debugCalls, exchanges *int) {
	t.Helper()
	t.Setenv("META_GRAPH_API_VERSION", "v25.0")
	previousDelay := metaInspectionRetryDelay
	metaInspectionRetryDelay = 0
	t.Cleanup(func() { metaInspectionRetryDelay = previousDelay })
	original := httpClient
	t.Cleanup(func() { httpClient = original })
	debugCalls, exchanges = new(int), new(int)
	httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v25.0/debug_token":
			*debugCalls++
			require.LessOrEqual(t, *debugCalls, len(steps), "unexpected extra debug_token call")
			return steps[*debugCalls-1](req)
		case "/v25.0/oauth/access_token":
			*exchanges++
			return jsonResponse(req, `{"access_token":"exchanged-token"}`), nil
		case "/v25.0/me/permissions":
			return jsonResponse(req, `{"data":[{"permission":"instagram_basic","status":"granted"}]}`), nil
		default:
			return jsonResponseWithStatus(req, 404, `{"error":{"message":"unexpected","code":803}}`), nil
		}
	})}
	return debugCalls, exchanges
}

func debugTokenStatus(status int) debugTokenStep {
	return func(req *http.Request) (*http.Response, error) {
		return jsonResponseWithStatus(req, status, `{"error":{"message":"inspection failed","code":1}}`), nil
	}
}

func debugTokenOK(tokenType string) debugTokenStep {
	return func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, debugTokenBody(tokenType, 0, unifiedScopes...)), nil
	}
}

func TestInstagramConfigurationInspectionRetriesOnlyTransientFailures(t *testing.T) {
	networkFailure := func(*http.Request) (*http.Response, error) { return nil, errors.New("connection reset") }

	for name, first := range map[string]debugTokenStep{
		"a 500":           debugTokenStatus(http.StatusInternalServerError),
		"a 503":           debugTokenStatus(http.StatusServiceUnavailable),
		"a network error": networkFailure,
	} {
		t.Run("one retry recovers from "+name, func(t *testing.T) {
			debugCalls, exchanges := scriptDebugToken(t, first, debugTokenOK("SYSTEM_USER"))
			token, err := configuredInstagramAdapter().ExchangeCode(t.Context(), "code", nil)
			require.NoError(t, err)
			require.Equal(t, ConnectionModeUnifiedLogin, token.Extra["connection_type"])
			require.Equal(t, 2, *debugCalls)
			require.Equal(t, 1, *exchanges, "the single-use code is not exchanged again")
		})
	}

	t.Run("a user token recovers and keeps the Page flow", func(t *testing.T) {
		debugCalls, _ := scriptDebugToken(t, debugTokenStatus(http.StatusBadGateway), debugTokenOK("USER"))
		token, err := configuredInstagramAdapter().ExchangeCode(t.Context(), "code", nil)
		require.NoError(t, err)
		require.NotContains(t, token.Extra, "connection_type")
		require.Equal(t, 2, *debugCalls)
	})

	t.Run("two transient failures stop the connection after one retry", func(t *testing.T) {
		debugCalls, _ := scriptDebugToken(t, debugTokenStatus(http.StatusServiceUnavailable), debugTokenStatus(http.StatusServiceUnavailable))
		_, err := configuredInstagramAdapter().ExchangeCode(t.Context(), "code", nil)
		require.Error(t, err)
		require.Equal(t, 2, *debugCalls)
	})

	for name, step := range map[string]debugTokenStep{
		"a rejected app token": debugTokenStatus(http.StatusBadRequest),
		"an auth failure":      debugTokenStatus(http.StatusUnauthorized),
		"throttling":           debugTokenStatus(http.StatusTooManyRequests),
		"an invalid token": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, `{"data":{"is_valid":false,"scopes":["instagram_basic"]}}`), nil
		},
		"an unreadable body": func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, `not-json`), nil
		},
	} {
		t.Run("no retry on "+name, func(t *testing.T) {
			debugCalls, _ := scriptDebugToken(t, step)
			_, err := configuredInstagramAdapter().ExchangeCode(t.Context(), "code", nil)
			require.Error(t, err)
			require.Equal(t, 1, *debugCalls)
		})
	}

	t.Run("no retry after the caller cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		debugCalls, _ := scriptDebugToken(t, func(*http.Request) (*http.Response, error) {
			cancel()
			return nil, context.Canceled
		})
		_, err := configuredInstagramAdapter().ExchangeCode(ctx, "code", nil)
		require.Error(t, err)
		require.Equal(t, 1, *debugCalls)
	})
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &output
}

func TestInstagramConfigurationLogsWhichEvidenceDecidedTheType(t *testing.T) {
	for _, tc := range []struct {
		name      string
		debug, me string
		want      []string
	}{
		{
			name: "debug_token system user", debug: debugTokenBody("SYSTEM_USER", 0, unifiedScopes...),
			want: []string{`connection="unified_login"`, `evidence="debug_token_type"`, `debug_token_type="SYSTEM_USER"`},
		},
		{
			name: "debug_token user", debug: debugTokenBody("USER", 0, "instagram_basic"),
			want: []string{`connection="page_flow"`, `evidence="debug_token_type"`, `debug_token_type="USER"`},
		},
		{
			name: "client business present", debug: debugTokenBody("", 0, unifiedScopes...),
			me:   `{"id":"system-user-1","client_business_id":"business-secret-id"}`,
			want: []string{`connection="unified_login"`, `evidence="client_business_id"`, `debug_token_type="absent"`},
		},
		{
			name: "client business missing", debug: debugTokenBody("", 0, "instagram_basic"),
			me:   `{"id":"person-1"}`,
			want: []string{`connection="page_flow"`, `evidence="no_client_business_id"`, `debug_token_type="absent"`},
		},
		{
			name: "field does not exist", debug: debugTokenBody("", 0, "instagram_basic"),
			me:   `400|{"error":{"message":"(#100) nonexisting field","code":100}}`,
			want: []string{`connection="page_flow"`, `evidence="client_business_id_unavailable"`, `debug_token_type="absent"`},
		},
		{
			name: "an unknown type is not echoed", debug: debugTokenBody("SURPRISE-secret", 0, "instagram_basic"),
			me:   `{"id":"person-1"}`,
			want: []string{`connection="page_flow"`, `evidence="no_client_business_id"`, `debug_token_type="other"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			responses := map[string]string{
				configExchangePath:          `{"access_token":"exchanged-token"}`,
				configDebugTokenPath:        tc.debug,
				"GET /v25.0/me/permissions": `{"data":[{"permission":"instagram_basic","status":"granted"}]}`,
			}
			if tc.me != "" {
				responses[configMePath] = tc.me
			}
			startFakeGraph(t, responses)
			output := captureLog(t)
			_, err := configuredInstagramAdapter().ExchangeCode(t.Context(), "code", nil)
			require.NoError(t, err)
			for _, field := range tc.want {
				require.Contains(t, output.String(), field)
			}
			for _, secret := range []string{"exchanged-token", "meta-secret", "meta-app", "business-secret-id", "system-user-1", "SURPRISE"} {
				require.NotContains(t, output.String(), secret)
			}
		})
	}
}

func TestInstagramNoPageMessageDependsOnTheConfiguration(t *testing.T) {
	pagesWithoutInstagram := `{"data":[{"id":"page-1","name":"Page","access_token":"page-token"}]}`

	t.Run("a configuration names both causes and the token type", func(t *testing.T) {
		startFakeGraph(t, map[string]string{"GET /v25.0/me/accounts": pagesWithoutInstagram})
		_, err := configuredInstagramAdapter().ListAccountSelections(t.Context(), &TokenResult{AccessToken: "user-token"})
		require.ErrorIs(t, err, ErrNoInstagramAccountForConfiguration)
		require.NotErrorIs(t, err, ErrNoInstagramProfessionalAccount)
		require.ErrorContains(t, err, "Facebook Page")
		require.ErrorContains(t, err, "token type")
	})

	t.Run("selection reports the same cause", func(t *testing.T) {
		startFakeGraph(t, map[string]string{"GET /v25.0/me/accounts": pagesWithoutInstagram})
		_, err := configuredInstagramAdapter().SelectAccount(t.Context(), &TokenResult{AccessToken: "user-token"}, "17841400000000123")
		require.ErrorIs(t, err, ErrNoInstagramAccountForConfiguration)
	})

	t.Run("an app without config_id keeps the Page message", func(t *testing.T) {
		startFakeGraph(t, map[string]string{"GET /v25.0/me/accounts": pagesWithoutInstagram})
		adapter := NewInstagramAdapter("meta-app", "meta-secret", "https://app.example/callback")
		_, err := adapter.ListAccountSelections(t.Context(), &TokenResult{AccessToken: "user-token"})
		require.ErrorIs(t, err, ErrNoInstagramProfessionalAccount)
		require.NotErrorIs(t, err, ErrNoInstagramAccountForConfiguration)
	})
}

func TestInstagramLoginPromptOnlyForConfigurations(t *testing.T) {
	promptOf := func(adapter *InstagramAdapter) string {
		authURL, _ := adapter.GenerateAuthURL("state-1")
		parsed, err := url.Parse(authURL)
		require.NoError(t, err)
		return parsed.Query().Get("prompt")
	}
	require.Equal(t, "login", promptOf(configuredInstagramAdapter()), "Meta: always show both the Facebook and Instagram options")
	require.Empty(t, promptOf(NewInstagramAdapter("meta-app", "meta-secret", "https://app.example/callback")))
	require.Empty(t, promptOf(NewInstagramLoginAdapter("ig-app", "ig-secret", "https://app.example/callback")))
}
