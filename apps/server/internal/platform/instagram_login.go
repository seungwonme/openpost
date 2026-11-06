package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/openpost/backend/internal/providerlimits"
)

var instagramLoginUserID = regexp.MustCompile(`^[1-9][0-9]*$`)

var (
	ErrInstagramLoginInvalidProfile      = errors.New("instagram login profile returned no unambiguous account")
	ErrInstagramLoginInvalidUserID       = errors.New("instagram login profile returned no valid user_id")
	ErrInstagramLoginProfessionalAccount = errors.New("instagram login requires a Business or Creator account")
)

// NewInstagramLoginAdapter uses Instagram's professional-account login without
// a Facebook Page. Delivery and durable checkpoints share the Instagram owner.
func NewInstagramLoginAdapter(clientID, clientSecret, redirectURI string) *InstagramAdapter {
	adapter := NewInstagramAdapter(clientID, clientSecret, redirectURI)
	adapter.instagramLogin = true
	return adapter
}

func (i *InstagramAdapter) validateLoginSettings(req *PublishRequest) error {
	unavailable := providerlimits.InstagramLoginSettingUnavailable
	switch {
	case i.instagramLogin:
	case i.unifiedLogin:
		unavailable = providerlimits.InstagramUnifiedSettingUnavailable
	default:
		return nil
	}
	settings := append([]map[string]interface{}{req.Settings}, req.MediaSettings...)
	for _, values := range settings {
		for key := range values {
			value := settingString(values, key)
			if unavailable(key) && value != "" && value != "[]" {
				return fmt.Errorf("instagram setting %s requires a Facebook Login connection", key)
			}
		}
	}
	return nil
}

func (i *InstagramAdapter) instagramScope(facebookScope string) string {
	if !i.instagramLogin {
		return facebookScope
	}
	switch facebookScope {
	case "instagram_basic":
		return "instagram_business_basic"
	case "instagram_content_publish":
		return "instagram_business_content_publish"
	case "instagram_manage_comments":
		return "instagram_business_manage_comments"
	case "instagram_manage_messages":
		return "instagram_business_manage_messages"
	case "instagram_manage_insights":
		return "instagram_business_manage_insights"
	default:
		return facebookScope
	}
}

func (i *InstagramAdapter) instagramLoginAuthURL(state string) string {
	query := url.Values{
		"client_id": {i.clientID}, "redirect_uri": {i.redirectURI},
		"response_type": {"code"}, "state": {state}, "enable_fb_login": {"0"},
		"scope": {strings.Join([]string{
			"instagram_business_basic", "instagram_business_content_publish",
			"instagram_business_manage_comments", "instagram_business_manage_messages",
			"instagram_business_manage_insights",
		}, ",")},
	}
	return "https://www.instagram.com/oauth/authorize?" + query.Encode()
}

func (i *InstagramAdapter) exchangeInstagramLoginCode(ctx context.Context, code string) (*TokenResult, error) {
	body, err := DoFormURLEncoded(ctx, http.MethodPost, "https://api.instagram.com/oauth/access_token", map[string]string{
		"client_id": i.clientID, "client_secret": i.clientSecret,
		"redirect_uri": i.redirectURI, "grant_type": "authorization_code", "code": code,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("instagram login token exchange: %w", err)
	}
	type codeToken struct {
		AccessToken string          `json:"access_token"`
		UserID      json.Number     `json:"user_id"`
		Permissions json.RawMessage `json:"permissions"`
	}
	var response struct {
		codeToken
		Data []codeToken `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decoding instagram login token: %w", err)
	}
	short := response.codeToken
	if len(response.Data) == 1 {
		short = response.Data[0]
	}
	if len(response.Data) > 1 || short.AccessToken == "" || short.UserID.String() == "" {
		return nil, fmt.Errorf("instagram login token exchange returned no unambiguous account token")
	}
	query := url.Values{
		"grant_type": {"ig_exchange_token"}, "client_secret": {i.clientSecret},
		"access_token": {short.AccessToken},
	}
	body, err = DoRequestNoRedirect(ctx, http.MethodGet, "https://graph.instagram.com/access_token?"+query.Encode(), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("instagram login long-lived token exchange: %w", err)
	}
	token, err := decodeFacebookToken("instagram login long-lived token exchange", body)
	if err != nil {
		return nil, err
	}
	if token.ExpiresIn <= 0 {
		return nil, fmt.Errorf("instagram login token exchange returned no expiry")
	}
	// Current responses include the actual grant. Older responses need the
	// permissions endpoint; never substitute the requested scope list.
	var scopes []string
	if len(short.Permissions) > 0 && string(short.Permissions) != "null" {
		var permissions string
		if err := json.Unmarshal(short.Permissions, &permissions); err == nil {
			scopes = strings.Fields(strings.ReplaceAll(permissions, ",", " "))
		} else if err := json.Unmarshal(short.Permissions, &scopes); err != nil {
			return nil, fmt.Errorf("decoding instagram login granted permissions: %w", err)
		}
	} else {
		scopes, err = fetchMetaGrantedScopes(ctx, i.graphURL, token.AccessToken)
		if err != nil {
			return nil, fmt.Errorf("instagram login granted permissions: %w", err)
		}
	}
	token = tokenWithGrantedScopes(token, scopes)
	token.Extra["user_id"] = short.UserID.String()
	return token, nil
}

func (i *InstagramAdapter) refreshInstagramLoginToken(ctx context.Context, input RefreshTokenInput) (*TokenResult, error) {
	if input.AccessToken == "" {
		return nil, fmt.Errorf("instagram login refresh requires an access token")
	}
	query := url.Values{"grant_type": {"ig_refresh_token"}, "access_token": {input.AccessToken}}
	body, err := DoRequestNoRedirect(ctx, http.MethodGet, "https://graph.instagram.com/refresh_access_token?"+query.Encode(), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("instagram login token refresh: %w", err)
	}
	token, err := decodeFacebookToken("instagram login token refresh", body)
	if err != nil {
		return nil, err
	}
	if token.ExpiresIn <= 0 {
		return nil, fmt.Errorf("instagram login refresh returned no expiry")
	}
	return token, nil
}

func (i *InstagramAdapter) instagramLoginProfile(ctx context.Context, accessToken string) (*UserProfile, error) {
	query := url.Values{
		"fields":       {"user_id,username,name,profile_picture_url,account_type"},
		"access_token": {accessToken},
	}
	body, err := doMetaRequest(ctx, http.MethodGet, i.graphURL("me")+"?"+query.Encode(), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("instagram login profile: %w", err)
	}
	type loginProfile struct {
		UserID      json.Number `json:"user_id"`
		Username    string      `json:"username"`
		Name        string      `json:"name"`
		Picture     string      `json:"profile_picture_url"`
		AccountType string      `json:"account_type"`
	}
	var response struct {
		loginProfile
		Data []loginProfile `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInstagramLoginInvalidProfile, err)
	}
	profile := response.loginProfile
	if response.Data != nil {
		if len(response.Data) != 1 || profile.UserID.String() != "" {
			return nil, ErrInstagramLoginInvalidProfile
		}
		profile = response.Data[0]
	}
	if !instagramLoginUserID.MatchString(profile.UserID.String()) {
		return nil, ErrInstagramLoginInvalidUserID
	}
	accountType := strings.ToLower(profile.AccountType)
	if accountType != "business" && accountType != "media_creator" && accountType != "creator" {
		return nil, ErrInstagramLoginProfessionalAccount
	}
	if accountType == "media_creator" {
		accountType = "creator"
	}
	return &UserProfile{
		ID: profile.UserID.String(), Username: profile.Username, DisplayName: profile.Name, AvatarURL: profile.Picture,
		CapabilityState: map[string]string{"connection_type": ConnectionModeInstagramLogin, "instagram_account_type": accountType},
	}, nil
}
