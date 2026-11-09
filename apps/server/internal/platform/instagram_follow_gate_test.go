package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// Shaped after the User Profile API sample response for
// GET /<INSTAGRAM_SCOPED_ID>?fields=...,is_user_follow_business,...
const instagramUserProfileFixture = `{
	"name": "Peter Chang",
	"username": "peter_chang_live",
	"profile_pic": "https://fbcdn-profile-a.akamaihd.net/hprofile-ak-xpf1/v/t1.0-1/p200x200/13055603_10105219398495383_8237637584159975445_n.jpg",
	"follower_count": 1234,
	"is_user_follow_business": false,
	"is_business_follow_user": true
}`

func TestInstagramUserProfileReadsFollowStatusInEveryLoginMode(t *testing.T) {
	original := httpClient
	t.Cleanup(func() { httpClient = original })
	for _, mode := range []struct {
		name    string
		host    string
		prepare func(*InstagramAdapter)
	}{
		{name: "facebook login", host: "graph.facebook.com", prepare: func(*InstagramAdapter) {}},
		{name: "unified login", host: "graph.facebook.com", prepare: func(a *InstagramAdapter) { a.unifiedLogin = true }},
		{name: "instagram login", host: "graph.instagram.com", prepare: func(a *InstagramAdapter) { a.instagramLogin = true }},
	} {
		t.Run(mode.name, func(t *testing.T) {
			adapter := NewInstagramAdapter("", "", "")
			mode.prepare(adapter)
			requests := 0
			httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				require.Equal(t, http.MethodGet, req.Method)
				require.Equal(t, mode.host, req.URL.Host)
				require.Equal(t, "/"+adapter.graphVersion+"/1784140000000001", req.URL.Path)
				require.Equal(t, "username,is_user_follow_business", req.URL.Query().Get("fields"))
				require.Empty(t, req.URL.Query().Get("access_token"), "the token travels in the Authorization header")
				require.Equal(t, "Bearer profile-token", req.Header.Get("Authorization"))
				return jsonResponse(req, instagramUserProfileFixture), nil
			})}

			profile, err := adapter.FetchMessagingUserProfile(context.Background(), "profile-token", "1784140000000001")

			require.NoError(t, err)
			require.Equal(t, 1, requests)
			require.Equal(t, MessagingUserProfile{Username: "peter_chang_live", FollowsAccount: false}, profile)
		})
	}
}

func TestInstagramUserProfileNeverGuessesTheFollowStatus(t *testing.T) {
	original := httpClient
	t.Cleanup(func() { httpClient = original })
	adapter := NewInstagramAdapter("", "", "")
	adapter.instagramLogin = true
	t.Run("a refused read keeps Meta's status and code", func(t *testing.T) {
		httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithStatus(req, http.StatusBadRequest, `{"error":{"message":"User consent is required to access user profile.","type":"IGApiException","code":230,"fbtrace_id":"trace"}}`), nil
		})}
		_, err := adapter.FetchMessagingUserProfile(context.Background(), "profile-token", "1784140000000001")
		var providerErr *HTTPError
		require.ErrorAs(t, err, &providerErr)
		require.Equal(t, http.StatusBadRequest, providerErr.StatusCode)
		require.Equal(t, "230", providerErr.Code)
	})
	t.Run("an answer without the field is not 'not following'", func(t *testing.T) {
		httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, `{"name":"Peter Chang","username":"peter_chang_live"}`), nil
		})}
		_, err := adapter.FetchMessagingUserProfile(context.Background(), "profile-token", "1784140000000001")
		require.True(t, errors.Is(err, ErrFollowStatusUnreported))
	})
}

// The documented button template request, POST /<IG_ID>/messages.
const instagramButtonTemplateFixture = `{
	"recipient": {"id": "1784140000000001"},
	"message": {
		"attachment": {
			"type": "template",
			"payload": {
				"template_type": "button",
				"text": "Here is your guide",
				"buttons": [{"type": "web_url", "url": "https://example.com/guide", "title": "Open guide"}]
			}
		}
	}
}`

func TestInstagramSendsAButtonTemplate(t *testing.T) {
	original := httpClient
	t.Cleanup(func() { httpClient = original })
	adapter := NewInstagramAdapter("", "", "")
	adapter.instagramLogin = true
	send := func(t *testing.T, input SendMessageRequest) map[string]any {
		t.Helper()
		var sent map[string]any
		httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, http.MethodPost, req.Method)
			require.Equal(t, "graph.instagram.com", req.URL.Host)
			require.Equal(t, "/"+adapter.graphVersion+"/owner/messages", req.URL.Path)
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(body, &sent))
			return jsonResponse(req, `{"recipient_id":"1784140000000001","message_id":"aWdfZAG1"}`), nil
		})}
		result, err := adapter.SendMessage(context.Background(), "send-token", input)
		require.NoError(t, err)
		require.Equal(t, "aWdfZAG1", result.RemoteMessageID)
		require.Equal(t, "send-token", sent["access_token"])
		delete(sent, "access_token")
		return sent
	}

	var want map[string]any
	require.NoError(t, json.Unmarshal([]byte(instagramButtonTemplateFixture), &want))
	require.Equal(t, want, send(t, SendMessageRequest{AccountID: "owner", CounterpartRemoteID: "1784140000000001", Body: " Here is your guide ", Buttons: []MessageButton{{Title: "Open guide", URL: "https://example.com/guide"}}}))

	plain := send(t, SendMessageRequest{AccountID: "owner", CounterpartRemoteID: "1784140000000001", Body: "hi"})
	require.Equal(t, map[string]any{"text": "hi"}, plain["message"], "a message without buttons is unchanged")
}

// The documented quick reply message, sent as a private reply: recipient is the
// comment, and tapping the reply posts its title as the person's message.
const instagramPrivateQuickReplyFixture = `{
	"recipient": {"comment_id": "17890000000000042"},
	"message": {
		"text": "Thanks for commenting! Tap the button once you follow us.",
		"quick_replies": [{"content_type": "text", "title": "팔로우했어요", "payload": "OPENPOST_FOLLOW_CHECK"}]
	}
}`

func TestInstagramPrivateReplyCarriesAQuickReply(t *testing.T) {
	original := httpClient
	t.Cleanup(func() { httpClient = original })
	adapter := NewInstagramAdapter("", "", "")
	adapter.instagramLogin = true
	quick := []QuickReply{{Title: "팔로우했어요", Payload: FollowCheckQuickReplyPayload}}
	t.Run("the request matches the documented payload", func(t *testing.T) {
		var sent map[string]any
		httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, http.MethodPost, req.Method)
			require.Equal(t, "graph.instagram.com", req.URL.Host)
			require.Equal(t, "/"+adapter.graphVersion+"/owner/messages", req.URL.Path)
			require.Equal(t, "Bearer reply-token", req.Header.Get("Authorization"))
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(body, &sent))
			return jsonResponse(req, `{"recipient_id":"1784140000000001","message_id":"aWdfZAG2"}`), nil
		})}
		id, err := adapter.ReplyPrivatelyToCommentWithQuickReplies(context.Background(), "reply-token", "owner", "17890000000000042", "Thanks for commenting! Tap the button once you follow us.", quick)
		require.NoError(t, err)
		require.Equal(t, "aWdfZAG2", id)
		var want map[string]any
		require.NoError(t, json.Unmarshal([]byte(instagramPrivateQuickReplyFixture), &want))
		require.Equal(t, want, sent)
	})
	t.Run("a rejection is returned, never retried without the quick reply", func(t *testing.T) {
		requests := 0
		httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			return jsonResponseWithStatus(req, http.StatusBadRequest, `{"error":{"message":"Invalid parameter","type":"IGApiException","code":100}}`), nil
		})}
		_, err := adapter.ReplyPrivatelyToCommentWithQuickReplies(context.Background(), "reply-token", "owner", "17890000000000042", "Tap the button", quick)
		var providerErr *HTTPError
		require.ErrorAs(t, err, &providerErr)
		require.Equal(t, http.StatusBadRequest, providerErr.StatusCode)
		require.Equal(t, 1, requests)
	})
	t.Run("a follow request DM repeats the quick reply", func(t *testing.T) {
		var sent map[string]any
		httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(body, &sent))
			return jsonResponse(req, `{"recipient_id":"1784140000000001","message_id":"aWdfZAG3"}`), nil
		})}
		_, err := adapter.SendMessage(context.Background(), "send-token", SendMessageRequest{AccountID: "owner", CounterpartRemoteID: "1784140000000001", Body: "Follow us first", QuickReplies: quick})
		require.NoError(t, err)
		require.Equal(t, map[string]any{"text": "Follow us first", "quick_replies": []any{map[string]any{"content_type": "text", "title": "팔로우했어요", "payload": "OPENPOST_FOLLOW_CHECK"}}}, sent["message"])
	})
}
