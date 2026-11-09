package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// InstagramCommentsFeed is the account activity feed that collects comments on
// media the professional account published outside OpenPost. Webhooks deliver
// the same comments once Meta grants Advanced Access; both paths share one
// Inbox identity.
const InstagramCommentsFeed = "comments"

// InstagramRecentMediaLimit bounds the media listing to one Graph request.
const InstagramRecentMediaLimit = 25

// InstagramMediaSummary is one of the account's own media items with the
// comment count Instagram reports for it.
type InstagramMediaSummary struct {
	ID            string
	Permalink     string
	PublishedAt   time.Time
	CommentsCount int
}

// InstagramNativeCommentReader lists recent media with their comment counts,
// so collection reads the comments of only the media whose count changed.
type InstagramNativeCommentReader interface {
	ListRecentMedia(ctx context.Context, token, accountID string) ([]InstagramMediaSummary, error)
	ListComments(ctx context.Context, token, accountID, mediaID string) ([]Comment, error)
}

// AccountActivitySupport requires media read access in addition to the
// comment permissions owned-post collection already needs.
func (i *InstagramAdapter) AccountActivitySupport() []AccountActivitySupport {
	scopes := []string{i.instagramScope("instagram_basic")}
	for _, scope := range i.EngagementSupport().RequiredScopes {
		if scope != scopes[0] {
			scopes = append(scopes, scope)
		}
	}
	return []AccountActivitySupport{{Feed: InstagramCommentsFeed, RequiredScopes: scopes}}
}

// ListAccountActivity is never paged: the engagement service collects this
// feed through ListRecentMedia and ListComments, keeping per-media counts.
func (i *InstagramAdapter) ListAccountActivity(context.Context, string, string, AccountActivityRequest) (AccountActivityPage, error) {
	return AccountActivityPage{}, errors.New("instagram comments are collected through recent media")
}

func (i *InstagramAdapter) ListRecentMedia(ctx context.Context, token, accountID string) ([]InstagramMediaSummary, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, errors.New("instagram account identity is missing")
	}
	query := url.Values{
		"fields": {"id,timestamp,permalink,comments_count,media_product_type"},
		"limit":  {strconv.Itoa(InstagramRecentMediaLimit)},
	}
	data, err := doMetaRequest(ctx, http.MethodGet, i.graphURL(url.PathEscape(accountID)+"/media")+"?"+query.Encode(), nil, bearerHeaders(token))
	if err = metaCommentReadError(err); err != nil {
		return nil, err
	}
	var response struct {
		Data []struct {
			ID            string `json:"id"`
			Timestamp     string `json:"timestamp"`
			Permalink     string `json:"permalink"`
			CommentsCount *int   `json:"comments_count"`
			Product       string `json:"media_product_type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode instagram media: %w", err)
	}
	if response.Data == nil || len(response.Data) > InstagramRecentMediaLimit {
		return nil, errors.New("invalid instagram media page")
	}
	media := make([]InstagramMediaSummary, 0, len(response.Data))
	for _, item := range response.Data {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" || len(item.ID) > 128 {
			return nil, errors.New("instagram media identity is invalid")
		}
		// Live media cannot start a comment campaign; the webhook reader refuses it too.
		if strings.EqualFold(item.Product, "LIVE") {
			continue
		}
		count := 0
		if item.CommentsCount != nil && *item.CommentsCount > 0 {
			count = *item.CommentsCount
		}
		media = append(media, InstagramMediaSummary{ID: item.ID, Permalink: item.Permalink, PublishedAt: ProviderTime(item.Timestamp), CommentsCount: count})
	}
	return media, nil
}
