package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// InstagramUnifiedProviderKey selects the adapter for accounts connected with a
// Business Integration System User token. The provider app stays the oauth
// entry: this is the account's connection type, not an application mode.
const InstagramUnifiedProviderKey = providerInstagram + ":" + ConnectionModeUnifiedLogin

const (
	metaDebugTokenSystemUser = "SYSTEM_USER"
	metaDebugTokenUser       = "USER"
	metaDebugTokenPage       = "PAGE"
	// Graph answers this code when a field does not exist on the /me node, which
	// is what a user token that has no client business can return.
	metaNonexistentFieldCode = "100"
)

// Evidence recorded for the first live connection. Fixed strings only.
const (
	metaTokenEvidenceDebugType            = "debug_token_type"
	metaTokenEvidenceClientBusiness       = "client_business_id"
	metaTokenEvidenceNoClientBusiness     = "no_client_business_id"
	metaTokenEvidenceClientBusinessAbsent = "client_business_id_unavailable"
)

// ErrNoInstagramAssignedAccount means the client business shared no Instagram
// professional account with this app during Unified Login.
var ErrNoInstagramAssignedAccount = errors.New("instagram unified login shared no professional account")

// ErrNoInstagramAccountForConfiguration replaces the plain Page error when a
// config_id connection took the Page path and found no account. Either cause
// fits: the account has no manageable Page, or the configuration issues a user
// token and needed a system user token.
var ErrNoInstagramAccountForConfiguration = errors.New("instagram found no account for this configuration: link the account to a Facebook Page you can manage, or check the configuration's token type")

// metaInspectionRetryDelay spaces the single retry of a token inspection.
var metaInspectionRetryDelay = 500 * time.Millisecond

// NewInstagramUnifiedLoginAdapter serves accounts whose token belongs to the
// client's business portfolio. Every call uses the Instagram account ID on
// graph.facebook.com, and no Facebook Page is involved.
func NewInstagramUnifiedLoginAdapter(clientID, clientSecret, redirectURI string) *InstagramAdapter {
	adapter := NewInstagramAdapter(clientID, clientSecret, redirectURI)
	adapter.unifiedLogin = true
	return adapter
}

func isInstagramUnifiedToken(token *TokenResult) bool {
	return token != nil && token.Extra["connection_type"] == ConnectionModeUnifiedLogin
}

// InstagramPrivateReplyScopes lists the grants a private reply needs. Only the
// Unified Login setup requires messaging permission and instagram_basic.
func InstagramPrivateReplyScopes(providerKey string) []string {
	switch providerKey {
	case InstagramUnifiedProviderKey:
		return []string{"instagram_basic", "instagram_manage_comments", "instagram_manage_messages"}
	case providerInstagram + ":" + ConnectionModeInstagramLogin:
		return []string{"instagram_business_manage_comments"}
	default:
		return []string{"instagram_manage_comments"}
	}
}

// InstagramUnifiedMessagingScopes lists the grants message intake and sync
// need. pages_manage_metadata does not apply: there is no Page.
func InstagramUnifiedMessagingScopes() []string {
	return []string{"instagram_basic", "instagram_manage_messages"}
}

// metaTokenInspection is the debug_token result. Type is absent from Meta's
// reference table, so an empty or unknown value falls back to client_business_id.
type metaTokenInspection struct {
	Type    string   `json:"type"`
	IsValid bool     `json:"is_valid"`
	Scopes  []string `json:"scopes"`
	// ExpiresAt is nil when Meta omits it. Zero states the token never expires.
	ExpiresAt *int64 `json:"expires_at"`
	// UserID is only the grant subject fallback, so its JSON type must not be
	// able to fail a connection.
	UserID json.RawMessage `json:"user_id"`
}

// exchangeConfigurationCode handles a Facebook Login for Business configuration.
// A configuration can yield a user token or a system user token, so the type is
// read from the token itself before anything depends on it. The long-lived
// exchange and /me/permissions belong to the user-token path only. Any failure
// to establish the type stops the connection instead of guessing.
func (i *InstagramAdapter) exchangeConfigurationCode(ctx context.Context, code string) (*TokenResult, error) {
	token, err := exchangeMetaCode(ctx, i.graphURL, i.clientID, i.clientSecret, i.redirectURI, "instagram", code)
	if err != nil {
		return nil, err
	}
	inspection, err := i.inspectMetaToken(ctx, token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("instagram token inspection: %w", err)
	}
	systemUser, evidence, err := i.isSystemUserToken(ctx, token.AccessToken, inspection)
	if err != nil {
		return nil, fmt.Errorf("instagram token type: %w", err)
	}
	logInstagramTokenClassification(systemUser, evidence, inspection.Type)
	if systemUser {
		return unifiedLoginToken(token, inspection)
	}
	if longLived, longLivedErr := exchangeMetaLongLivedToken(ctx, i.graphURL, i.clientID, i.clientSecret, "instagram", token.AccessToken); longLivedErr == nil {
		token = longLived
	}
	scopes, err := fetchMetaGrantedScopes(ctx, i.graphURL, token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("instagram granted permissions: %w", err)
	}
	return tokenWithGrantedScopes(token, scopes), nil
}

// logInstagramTokenClassification never receives a token. The Meta type is
// reduced to known values so provider text cannot reach the log.
func logInstagramTokenClassification(systemUser bool, evidence, debugType string) {
	connection := "page_flow"
	if systemUser {
		connection = ConnectionModeUnifiedLogin
	}
	reported := "other"
	switch normalized := strings.ToUpper(strings.TrimSpace(debugType)); normalized {
	case "":
		reported = "absent"
	case metaDebugTokenSystemUser, metaDebugTokenUser, metaDebugTokenPage:
		reported = normalized
	}
	log.Printf("[Instagram Token] connection=%q evidence=%q debug_token_type=%q", connection, evidence, reported)
}

// isTransientMetaFailure is limited to 5xx responses and network failures. A
// cancelled request and every other answer from Meta are final.
func isTransientMetaFailure(err error) bool {
	var providerErr *HTTPError
	if errors.As(err, &providerErr) {
		return providerErr.StatusCode >= http.StatusInternalServerError
	}
	var transportErr *TransportError
	return errors.As(err, &transportErr) && transportErr.Kind != TransportFailureCanceled
}

func (i *InstagramAdapter) inspectMetaToken(ctx context.Context, accessToken string) (metaTokenInspection, error) {
	query := url.Values{
		"input_token":         {accessToken},
		oauthParamAccessToken: {i.clientID + "|" + i.clientSecret},
	}
	endpoint := i.graphURL("debug_token") + "?" + query.Encode()
	body, err := doMetaRequest(ctx, http.MethodGet, endpoint, nil, nil)
	if isTransientMetaFailure(err) && ctx.Err() == nil {
		// The authorization code is already spent, so a failure here costs the
		// person the whole login. The inspection is a safe read to repeat once.
		select {
		case <-ctx.Done():
			return metaTokenInspection{}, ctx.Err()
		case <-time.After(metaInspectionRetryDelay):
		}
		body, err = doMetaRequest(ctx, http.MethodGet, endpoint, nil, nil)
	}
	if err != nil {
		return metaTokenInspection{}, err
	}
	var response struct {
		Data struct {
			metaTokenInspection
			Error json.RawMessage `json:"error"`
		} `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return metaTokenInspection{}, errors.New("meta returned an unreadable token inspection")
	}
	for _, embedded := range []json.RawMessage{response.Error, response.Data.Error} {
		if err := metaGraphEmbeddedError(embedded); err != nil {
			return metaTokenInspection{}, err
		}
	}
	if !response.Data.IsValid {
		return metaTokenInspection{}, errors.New("meta reports the access token is not valid")
	}
	return response.Data.metaTokenInspection, nil
}

// isSystemUserToken prefers an explicit debug_token type. Otherwise it uses the
// documented property of these tokens: /me carries a client_business_id.
func (i *InstagramAdapter) isSystemUserToken(ctx context.Context, accessToken string, inspection metaTokenInspection) (systemUser bool, evidence string, err error) {
	switch strings.ToUpper(strings.TrimSpace(inspection.Type)) {
	case metaDebugTokenSystemUser:
		return true, metaTokenEvidenceDebugType, nil
	case metaDebugTokenUser, metaDebugTokenPage:
		return false, metaTokenEvidenceDebugType, nil
	}
	query := url.Values{"fields": {"id,client_business_id"}, oauthParamAccessToken: {accessToken}}
	body, err := doMetaRequest(ctx, http.MethodGet, i.graphURL("me")+"?"+query.Encode(), nil, nil)
	if err != nil {
		var providerErr *HTTPError
		if errors.As(err, &providerErr) && providerErr.Code == metaNonexistentFieldCode {
			return false, metaTokenEvidenceClientBusinessAbsent, nil
		}
		return false, "", err
	}
	var response struct {
		ClientBusinessID string          `json:"client_business_id"`
		Error            json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return false, "", errors.New("meta returned an unreadable profile")
	}
	if err := metaGraphEmbeddedError(response.Error); err != nil {
		var providerErr *HTTPError
		if errors.As(err, &providerErr) && providerErr.Code == metaNonexistentFieldCode {
			return false, metaTokenEvidenceClientBusinessAbsent, nil
		}
		return false, "", err
	}
	if strings.TrimSpace(response.ClientBusinessID) == "" {
		return false, metaTokenEvidenceNoClientBusiness, nil
	}
	return true, metaTokenEvidenceClientBusiness, nil
}

// unifiedLoginToken stores the expiry the inspection states and records the grant
// Meta reports. An empty scope list is a failure, never an implied grant.
func unifiedLoginToken(token *TokenResult, inspection metaTokenInspection) (*TokenResult, error) {
	if len(inspection.Scopes) == 0 {
		return nil, errors.New("instagram token inspection reported no granted permissions")
	}
	if inspection.ExpiresAt != nil {
		// The inspection outranks the exchange's expires_in. Nothing can refresh
		// this token, so a stale expiry would end a working connection.
		token.ExpiresIn = 0
		if *inspection.ExpiresAt != 0 {
			remaining := time.Until(time.Unix(*inspection.ExpiresAt, 0))
			if remaining <= 0 {
				return nil, errors.New("meta reports the access token has already expired")
			}
			token.ExpiresIn = int(remaining.Seconds())
		}
	}
	token.RefreshToken = ""
	token = tokenWithGrantedScopes(token, inspection.Scopes)
	token.Extra["connection_type"] = ConnectionModeUnifiedLogin
	if subject := strings.Trim(string(inspection.UserID), `" `); subject != "" && subject != "null" {
		token.Extra["user_id"] = subject
	}
	return token, nil
}

type instagramAssignedAccount struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// listAssignedInstagramAccounts requests only the fields Meta documents for
// this edge. Selection hydrates the rest from the Instagram account node.
func (i *InstagramAdapter) listAssignedInstagramAccounts(ctx context.Context, token *TokenResult) ([]instagramAssignedAccount, error) {
	query := url.Values{
		"fields":              {"id,username"},
		"limit":               {"100"},
		oauthParamAccessToken: {token.AccessToken},
	}
	items, err := fetchMetaGraphEdge[instagramAssignedAccount](
		ctx, i.graphURL("me/assigned_instagram_accounts")+"?"+query.Encode(), "instagram assigned accounts", &metaGraphBudget{},
	)
	if err != nil {
		return nil, fmt.Errorf("instagram accounts: %w", err)
	}
	accounts := make([]instagramAssignedAccount, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.ID) != "" {
			accounts = append(accounts, item)
		}
	}
	if len(accounts) == 0 {
		return nil, ErrNoInstagramAssignedAccount
	}
	return accounts, nil
}

func (i *InstagramAdapter) listUnifiedAccountSelections(ctx context.Context, token *TokenResult) ([]AccountSelectionOption, error) {
	accounts, err := i.listAssignedInstagramAccounts(ctx, token)
	if err != nil {
		return nil, err
	}
	options := make([]AccountSelectionOption, 0, len(accounts))
	for _, account := range accounts {
		options = append(options, AccountSelectionOption{
			ID:          account.ID,
			Username:    firstNonEmptyString(account.Username, account.ID),
			DisplayName: firstNonEmptyString(account.Username, account.ID),
			Kind:        "Instagram professional",
			Description: "Business or Creator account",
		})
	}
	return options, nil
}

// selectUnifiedAccount re-reads the assignment so only an account the business
// shared can be stored. Name and picture are best effort: metadata refresh
// repairs them later, and a missing picture must not fail the connection.
func (i *InstagramAdapter) selectUnifiedAccount(ctx context.Context, token *TokenResult, selectionID string) (*SelectedAccount, error) {
	accounts, err := i.listAssignedInstagramAccounts(ctx, token)
	if err != nil {
		return nil, err
	}
	for _, account := range accounts {
		if account.ID != selectionID {
			continue
		}
		username, avatarURL := firstNonEmptyString(account.Username, account.ID), ""
		profile, profileErr := i.RefreshAccountMetadata(ctx, token.AccessToken, AccountMetadataRequest{AccountID: account.ID})
		if profileErr == nil && profile != nil {
			username = firstNonEmptyString(profile.Username, username)
			avatarURL = profile.AvatarURL
		}
		selected := *token
		selected.RefreshToken = ""
		selected.Extra = make(map[string]string, len(token.Extra)+1)
		for key, value := range token.Extra {
			selected.Extra[key] = value
		}
		selected.Extra["instagram_business_account_id"] = account.ID
		return &SelectedAccount{
			AccountID:        account.ID,
			AccountUsername:  username,
			AccountAvatarURL: avatarURL,
			Token:            &selected,
			CapabilityState: map[string]string{
				"connection_type":        ConnectionModeUnifiedLogin,
				"instagram_account_type": "professional",
			},
		}, nil
	}
	return nil, fmt.Errorf("instagram account selection %s was not found", selectionID)
}
