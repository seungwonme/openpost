package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
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
var ErrInstagramWebhookUnsupported = errors.New("instagram webhook subscription requires the linked Facebook Page")

// InstagramWebhookSubscription names one connected account. PageID is only
// used by Facebook Login, whose edge belongs to the linked Page.
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
//
// Unified Login needs a Business Integration System User token that OpenPost
// never holds, so it has no path here. The call is idempotent at Meta.
func (i *InstagramAdapter) SubscribeInstagramWebhooks(ctx context.Context, in InstagramWebhookSubscription) error {
	owner := in.AccountID
	if !i.instagramLogin {
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
	Code      string
	Message   string
	Retryable bool
}

// ClassifyInstagramWebhookError never includes provider response text.
func ClassifyInstagramWebhookError(err error) InstagramWebhookFailure {
	if errors.Is(err, ErrInstagramWebhookUnsupported) {
		return InstagramWebhookFailure{Code: InstagramWebhookUnsupportedConnection,
			Message: "This connection cannot be subscribed to Instagram webhooks. Reconnect the account with Instagram Login or Facebook Login with a linked Page."}
	}
	var providerErr *HTTPError
	if !errors.As(err, &providerErr) {
		return InstagramWebhookFailure{Code: InstagramWebhookUnavailable, Retryable: true,
			Message: "Instagram could not be reached to subscribe this account to webhooks. Toggle the feature off and on, or reconnect the account, to retry."}
	}
	switch {
	case providerErr.StatusCode == http.StatusUnauthorized || providerErr.Code == "190" || providerErr.Code == "102":
		return InstagramWebhookFailure{Code: InstagramWebhookAuthRequired,
			Message: "Instagram rejected the account token. Reconnect the account to receive comment and message webhooks."}
	case providerErr.StatusCode == http.StatusTooManyRequests || providerErr.Code == "4" || providerErr.Code == "17" || providerErr.Code == "32" || providerErr.Code == "613":
		return InstagramWebhookFailure{Code: InstagramWebhookRateLimited, Retryable: true,
			Message: "Instagram is rate limiting this app. Retry later by toggling the feature off and on, or reconnecting."}
	case providerErr.StatusCode >= http.StatusInternalServerError:
		return InstagramWebhookFailure{Code: InstagramWebhookUnavailable, Retryable: true,
			Message: "Instagram is temporarily unavailable. Retry later by toggling the feature off and on, or reconnecting."}
	case providerErr.StatusCode == http.StatusForbidden || providerErr.Code == "10" || providerErr.Code == "3" || providerErr.Code == "200":
		return InstagramWebhookFailure{Code: InstagramWebhookPermissionDenied,
			Message: "Meta denied the webhook subscription. Check that the Meta app is Live with Advanced Access for comments, then reconnect the account."}
	default:
		return InstagramWebhookFailure{Code: InstagramWebhookRejected,
			Message: "Meta rejected the webhook subscription. Check the app webhook settings and that the account is a public professional account, then reconnect."}
	}
}
