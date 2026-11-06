package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
)

// InstagramWebhookFields are the app webhook fields OpenPost consumes. Meta
// delivers an account's events only after the app subscribes that account.
const InstagramWebhookFields = "comments,messages"

// Failure codes stored with a subscription attempt. They are safe to show.
const (
	InstagramWebhookUnsupportedConnection = "unsupported_connection"
	InstagramWebhookAuthRequired          = "authentication_required"
	InstagramWebhookPermissionDenied      = "permission_denied"
	InstagramWebhookRateLimited           = "rate_limited"
	InstagramWebhookUnavailable           = "temporarily_unavailable"
	InstagramWebhookRejected              = "subscription_rejected"
)

// ErrInstagramWebhookUnsupported means the connection has no documented
// subscription edge, or the data needed to address it is missing.
var ErrInstagramWebhookUnsupported = errors.New("instagram webhook subscription is missing the account or token it needs")

// InstagramWebhookSubscription names one connected account. PageID is only
// used by Facebook Login, whose edge belongs to the linked Page. Instagram
// Login and Unified Login accounts have no Page.
type InstagramWebhookSubscription struct {
	AccessToken string
	AccountID   string
	PageID      string
}

// InstagramWebhookSubscriber is implemented by adapters whose connection mode
// has a documented /subscribed_apps edge.
type InstagramWebhookSubscriber interface {
	SubscribeInstagramWebhooks(context.Context, InstagramWebhookSubscription) error
}

// SubscribeInstagramWebhooks posts the subscription for the adapter's
// connection mode:
//   - Instagram Login: graph.instagram.com/{ig-id}/subscribed_apps, Instagram
//     User token.
//   - Facebook Login: graph.facebook.com/{page-id}/subscribed_apps, Page token.
//   - Unified Login: graph.facebook.com/{ig-id}/subscribed_apps, the Business
//     Integration System User token from the code exchange.
//
// The call is idempotent at Meta.
func (i *InstagramAdapter) SubscribeInstagramWebhooks(ctx context.Context, in InstagramWebhookSubscription) error {
	owner := in.AccountID
	if !i.instagramLogin && !i.unifiedLogin {
		owner = in.PageID
		if owner == "" {
			return ErrInstagramWebhookUnsupported
		}
	}
	if !instagramLoginUserID.MatchString(owner) || in.AccessToken == "" {
		return ErrInstagramWebhookUnsupported
	}
	endpoint := i.graphURL(owner+"/subscribed_apps") + "?" + url.Values{"subscribed_fields": {InstagramWebhookFields}}.Encode()
	body, err := doMetaRequest(ctx, http.MethodPost, endpoint, nil, map[string]string{headerAuthorization: bearerPrefix + in.AccessToken})
	if err != nil {
		return err
	}
	var response struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(body, &response); err != nil || !response.Success {
		return errors.New("instagram did not confirm the webhook subscription")
	}
	return nil
}

// InstagramWebhookFailure is a token-free, user-actionable classification.
type InstagramWebhookFailure struct {
	Code    string
	Message string
}

const instagramWebhookRetryHint = "Retry by toggling the feature off and on, or by reconnecting the account."

// ClassifyInstagramWebhookError never includes provider response text.
func ClassifyInstagramWebhookError(err error) InstagramWebhookFailure {
	if errors.Is(err, ErrInstagramWebhookUnsupported) {
		return InstagramWebhookFailure{Code: InstagramWebhookUnsupportedConnection,
			Message: "This connection cannot be subscribed to Instagram webhooks. Reconnect the account with Instagram Login, Unified Login, or Facebook Login with a linked Page."}
	}
	var providerErr *HTTPError
	if !errors.As(err, &providerErr) {
		return InstagramWebhookFailure{Code: InstagramWebhookUnavailable,
			Message: "Instagram could not be reached to subscribe this account to webhooks. " + instagramWebhookRetryHint}
	}
	for _, rule := range instagramWebhookRules {
		if rule.matches(providerErr) {
			return InstagramWebhookFailure{Code: rule.code, Message: rule.message}
		}
	}
	return InstagramWebhookFailure{Code: InstagramWebhookRejected,
		Message: "Meta rejected the webhook subscription. Check the app webhook settings and that the account is a public professional account, then reconnect."}
}

type instagramWebhookRule struct {
	statuses []int
	minState int
	codes    []string
	code     string
	message  string
}

func (r instagramWebhookRule) matches(err *HTTPError) bool {
	return slices.Contains(r.statuses, err.StatusCode) || slices.Contains(r.codes, err.Code) ||
		(r.minState > 0 && err.StatusCode >= r.minState)
}

// Order matters: the first matching rule wins.
var instagramWebhookRules = []instagramWebhookRule{
	{statuses: []int{http.StatusUnauthorized}, codes: []string{"190", "102"}, code: InstagramWebhookAuthRequired,
		message: "Instagram rejected the account token. Reconnect the account to receive comment and message webhooks."},
	{statuses: []int{http.StatusTooManyRequests}, codes: []string{"4", "17", "32", "613"}, code: InstagramWebhookRateLimited,
		message: "Instagram is rate limiting this app. " + instagramWebhookRetryHint},
	{minState: http.StatusInternalServerError, code: InstagramWebhookUnavailable,
		message: "Instagram is temporarily unavailable. " + instagramWebhookRetryHint},
	{statuses: []int{http.StatusForbidden}, codes: []string{"10", "3", "200"}, code: InstagramWebhookPermissionDenied,
		message: "Meta denied the webhook subscription. Check that the Meta app is Live with Advanced Access for comments, then reconnect the account."},
}
