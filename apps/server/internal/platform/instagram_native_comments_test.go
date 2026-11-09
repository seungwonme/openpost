package platform

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Fixture shaped after GET /{ig-user-id}/media with the comments_count field:
// a reel, a feed post whose count Instagram omits, and an ended live video.
const instagramRecentMediaFixture = `{
	"data": [
		{"id":"17900000000000001","timestamp":"2026-11-08T09:30:00+0000","permalink":"https://www.instagram.com/reel/AAA/","comments_count":3,"media_product_type":"REELS"},
		{"id":"17900000000000002","timestamp":"2026-11-07T18:00:00+0000","permalink":"https://www.instagram.com/p/BBB/","media_product_type":"FEED"},
		{"id":"17900000000000003","timestamp":"2026-11-06T12:00:00+0000","permalink":"https://www.instagram.com/p/CCC/","comments_count":9,"media_product_type":"LIVE"}
	],
	"paging": {"cursors": {"before":"b","after":"a"}, "next":"https://graph.facebook.com/v23.0/owner/media?after=a"}
}`

func TestInstagramRecentMediaReadsOneBoundedPageInEveryLoginMode(t *testing.T) {
	original := httpClient
	t.Cleanup(func() { httpClient = original })
	for _, mode := range []struct {
		name    string
		host    string
		scopes  []string
		prepare func(*InstagramAdapter)
	}{
		{name: "facebook login", host: "graph.facebook.com", scopes: []string{"instagram_basic", "instagram_manage_comments", "pages_read_engagement"}, prepare: func(*InstagramAdapter) {}},
		{name: "unified login", host: "graph.facebook.com", scopes: []string{"instagram_basic", "instagram_manage_comments"}, prepare: func(a *InstagramAdapter) { a.unifiedLogin = true }},
		{name: "instagram login", host: "graph.instagram.com", scopes: []string{"instagram_business_basic", "instagram_business_manage_comments"}, prepare: func(a *InstagramAdapter) { a.instagramLogin = true }},
	} {
		t.Run(mode.name, func(t *testing.T) {
			adapter := NewInstagramAdapter("", "", "")
			mode.prepare(adapter)
			requests := 0
			httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				require.Equal(t, http.MethodGet, req.Method)
				require.Equal(t, mode.host, req.URL.Host)
				require.Equal(t, "/"+adapter.graphVersion+"/owner/media", req.URL.Path)
				require.Equal(t, "id,timestamp,permalink,comments_count,media_product_type", req.URL.Query().Get("fields"))
				require.Equal(t, "25", req.URL.Query().Get("limit"))
				require.Empty(t, req.URL.Query().Get("access_token"), "the token travels in the Authorization header")
				require.Equal(t, "Bearer media-token", req.Header.Get("Authorization"))
				return jsonResponse(req, instagramRecentMediaFixture), nil
			})}

			media, err := adapter.ListRecentMedia(context.Background(), "media-token", "owner")

			require.NoError(t, err)
			require.Equal(t, 1, requests, "the continuation is never followed")
			require.Equal(t, []InstagramMediaSummary{
				{ID: "17900000000000001", Permalink: "https://www.instagram.com/reel/AAA/", PublishedAt: time.Date(2026, 11, 8, 9, 30, 0, 0, time.UTC), CommentsCount: 3},
				{ID: "17900000000000002", Permalink: "https://www.instagram.com/p/BBB/", PublishedAt: time.Date(2026, 11, 7, 18, 0, 0, 0, time.UTC), CommentsCount: 0},
			}, normalizeMediaTimes(media))
			require.Equal(t, []AccountActivitySupport{{Feed: InstagramCommentsFeed, RequiredScopes: mode.scopes}}, adapter.AccountActivitySupport())
		})
	}
}

func TestInstagramRecentMediaKeepsProviderPermissionErrors(t *testing.T) {
	original := httpClient
	t.Cleanup(func() { httpClient = original })
	httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponseWithStatus(req, http.StatusForbidden, `{"error":{"message":"(#10) Application does not have permission for this action","type":"OAuthException","code":10}}`), nil
	})}
	_, err := NewInstagramAdapter("", "", "").ListRecentMedia(context.Background(), "media-token", "owner")
	var providerErr *HTTPError
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, http.StatusForbidden, providerErr.StatusCode, "a missing permission must surface as reconnect-required")
}

func normalizeMediaTimes(media []InstagramMediaSummary) []InstagramMediaSummary {
	for index := range media {
		media[index].PublishedAt = media[index].PublishedAt.UTC()
	}
	return media
}
