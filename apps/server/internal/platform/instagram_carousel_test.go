package platform

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// instagramCarouselFixture is a fake Graph transport that records every
// container-creation form and answers from a per-call script.
type instagramCarouselFixture struct {
	t        *testing.T
	creates  []url.Values
	requests []string
	// createReply returns the HTTP status and body for the nth media create.
	createReply func(call int) (int, string)
}

func (f *instagramCarouselFixture) install(t *testing.T) {
	t.Helper()
	t.Setenv("META_GRAPH_API_VERSION", "v25.0")
	original := httpClient
	t.Cleanup(func() { httpClient = original })
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/v25.0/ig-1/media":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				f.t.Fatalf("reading create body: %v", err)
			}
			form, err := url.ParseQuery(string(body))
			if err != nil {
				f.t.Fatalf("parsing create form: %v", err)
			}
			f.creates = append(f.creates, form)
			status, reply := f.createReply(len(f.creates))
			return jsonResponseWithStatus(r, status, reply), nil
		case r.URL.Path == "/v25.0/ig-1/media_publish":
			return jsonResponse(r, `{"id":"post-1"}`), nil
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v25.0/container-"):
			return jsonResponse(r, `{"status_code":"FINISHED"}`), nil
		default:
			f.t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})}
}

func (f *instagramCarouselFixture) published() bool {
	for _, request := range f.requests {
		if request == "POST /v25.0/ig-1/media_publish" {
			return true
		}
	}
	return false
}

func instagramCarouselRequest(mimeTypes ...string) *PublishRequest {
	req := &PublishRequest{Content: "Carousel", Profile: "carousel", OutputProfile: "instagram.carousel"}
	for index, mimeType := range mimeTypes {
		extension := ".jpg"
		if strings.HasPrefix(mimeType, "video/") {
			extension = ".mp4"
		}
		req.PlatformMediaIDs = append(req.PlatformMediaIDs, "https://media.example/item-"+string(rune('a'+index))+extension)
		req.Media = append(req.Media, MediaItem{MimeType: mimeType})
	}
	return req
}

func TestInstagramCarouselVideoChildSendsVideoMediaType(t *testing.T) {
	fixture := &instagramCarouselFixture{t: t, createReply: func(call int) (int, string) {
		return http.StatusOK, `{"id":"container-` + string(rune('0'+call)) + `"}`
	}}
	fixture.install(t)

	req := instagramCarouselRequest("image/jpeg", "video/mp4", "video/quicktime")
	result, err := NewInstagramAdapter("", "", "").Publish(t.Context(), "token", "ig-1", req)
	if err != nil {
		t.Fatalf("publishing a mixed carousel: %v", err)
	}
	if result.ExternalID != "post-1" {
		t.Fatalf("unexpected result %#v", result)
	}
	if len(fixture.creates) != 4 {
		t.Fatalf("expected 3 child creates and 1 parent create, got %d", len(fixture.creates))
	}

	image := fixture.creates[0]
	if image.Get("image_url") != req.PlatformMediaIDs[0] || image.Get("is_carousel_item") != "true" ||
		image.Has("media_type") || image.Has("video_url") || image.Has("caption") {
		t.Fatalf("image child must stay an untyped image item, got %s", image.Encode())
	}
	for index := 1; index <= 2; index++ {
		video := fixture.creates[index]
		if video.Get("video_url") != req.PlatformMediaIDs[index] || video.Get("media_type") != "VIDEO" ||
			video.Get("is_carousel_item") != "true" || video.Has("image_url") || video.Has("caption") {
			t.Fatalf("video child %d must be a VIDEO carousel item, got %s", index, video.Encode())
		}
	}
	parent := fixture.creates[3]
	if parent.Get("media_type") != "CAROUSEL" || parent.Get("children") != "container-1,container-2,container-3" {
		t.Fatalf("unexpected carousel container form %s", parent.Encode())
	}
}

func TestInstagramStandaloneVideoMediaTypesUnchanged(t *testing.T) {
	cases := map[string]struct {
		profile, output, want string
	}{
		"feed video": {want: ""},
		"reel":       {profile: "short_video", output: "instagram.reel", want: "REELS"},
		"story":      {profile: "story", output: "instagram.story", want: "STORIES"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			fixture := &instagramCarouselFixture{t: t, createReply: func(int) (int, string) {
				return http.StatusOK, `{"id":"container-1"}`
			}}
			fixture.install(t)
			req := instagramCarouselRequest("video/mp4")
			req.Profile, req.OutputProfile = test.profile, test.output
			if _, err := NewInstagramAdapter("", "", "").Publish(t.Context(), "token", "ig-1", req); err != nil {
				t.Fatalf("publishing: %v", err)
			}
			form := fixture.creates[0]
			if form.Get("media_type") != test.want || form.Has("is_carousel_item") {
				t.Fatalf("standalone video form changed: %s", form.Encode())
			}
		})
	}
}

func TestInstagramCarouselChildRejectionIsDefinite(t *testing.T) {
	fixture := &instagramCarouselFixture{t: t, createReply: func(int) (int, string) {
		return http.StatusBadRequest, `{"error":{"code":100,"message":"Invalid parameter"}}`
	}}
	fixture.install(t)

	req := instagramCarouselRequest("video/mp4", "video/mp4")
	var checkpoints []PublishResult
	req.SetWriteFence(func(PublishResult) error { return nil }, func(result PublishResult) error {
		checkpoints = append(checkpoints, result)
		return nil
	})
	result, err := NewInstagramAdapter("", "", "").Publish(t.Context(), "token", "ig-1", req)
	if err == nil {
		t.Fatal("expected the child container rejection to surface")
	}
	var providerErr *HTTPError
	if !errors.As(err, &providerErr) || providerErr.StatusCode != http.StatusBadRequest || providerErr.Code != "meta:100" {
		t.Fatalf("provider error must be preserved, got %v", err)
	}
	if result.SubmissionState != PublishSubmissionRejected || result.RetrySafety != PublishRetryNever {
		t.Fatalf("child container 400 must be a definite rejection, got %#v", result)
	}
	if result.ProviderReference != "ig1:d:-" {
		t.Fatalf("rejection must retain the child-create checkpoint, got %q", result.ProviderReference)
	}
	if len(checkpoints) != 1 || checkpoints[0].SubmissionState != PublishSubmissionPending {
		t.Fatalf("only the pre-call checkpoint is expected, got %#v", checkpoints)
	}
	if len(fixture.creates) != 1 || fixture.published() {
		t.Fatalf("a rejected child must stop the sequence, requests: %v", fixture.requests)
	}
}

func TestInstagramCarouselParentRejectionIsDefinite(t *testing.T) {
	fixture := &instagramCarouselFixture{t: t, createReply: func(call int) (int, string) {
		if call <= 2 {
			return http.StatusOK, `{"id":"container-` + string(rune('0'+call)) + `"}`
		}
		return http.StatusBadRequest, `{"error":{"code":100,"message":"Invalid parameter"}}`
	}}
	fixture.install(t)

	req := instagramCarouselRequest("image/jpeg", "video/mp4")
	result, err := NewInstagramAdapter("", "", "").Publish(t.Context(), "token", "ig-1", req)
	var providerErr *HTTPError
	if !errors.As(err, &providerErr) || providerErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("provider error must be preserved, got %v", err)
	}
	if result.SubmissionState != PublishSubmissionRejected || result.ProviderReference != "ig1:a:container-1,container-2" {
		t.Fatalf("parent container 400 must be a definite rejection at the parent checkpoint, got %#v", result)
	}
	if fixture.published() {
		t.Fatal("a rejected carousel container must never be published")
	}
}

func TestInstagramCarouselTransientChildFailureStaysPending(t *testing.T) {
	fixture := &instagramCarouselFixture{t: t, createReply: func(int) (int, string) {
		return http.StatusTooManyRequests, `{"error":{"code":4,"message":"rate limit"}}`
	}}
	fixture.install(t)

	result, err := NewInstagramAdapter("", "", "").Publish(t.Context(), "token", "ig-1", instagramCarouselRequest("video/mp4", "video/mp4"))
	if err == nil {
		t.Fatal("expected the rate limit to surface")
	}
	if result.SubmissionState == PublishSubmissionRejected {
		t.Fatalf("a rate limited child create is not a definite rejection: %#v", result)
	}
}

func TestInstagramResumeCarouselChildCreateCheckpointRecreatesChild(t *testing.T) {
	fixture := &instagramCarouselFixture{t: t, createReply: func(int) (int, string) {
		return http.StatusOK, `{"id":"container-9"}`
	}}
	fixture.install(t)

	req := instagramCarouselRequest("video/mp4", "video/mp4")
	result, err := NewInstagramAdapter("", "", "").ResumePublish(t.Context(), "token", "ig-1", req, "ig1:d:-")
	if err != nil {
		t.Fatalf("resuming a child-create checkpoint: %v", err)
	}
	if len(fixture.creates) != 1 || fixture.creates[0].Get("media_type") != "VIDEO" || fixture.creates[0].Get("video_url") != req.PlatformMediaIDs[0] {
		t.Fatalf("the first child must be created again as a VIDEO item, got %v", fixture.creates)
	}
	if result.SubmissionState != PublishSubmissionPending || result.ProviderReference != "ig1:c:container-9" {
		t.Fatalf("unexpected resumed result %#v", result)
	}
}

func TestInstagramResumeCarouselChildCreateRejectionIsDefinite(t *testing.T) {
	fixture := &instagramCarouselFixture{t: t, createReply: func(int) (int, string) {
		return http.StatusBadRequest, `{"error":{"code":100,"message":"Invalid parameter"}}`
	}}
	fixture.install(t)

	result, err := NewInstagramAdapter("", "", "").ResumePublish(t.Context(), "token", "ig-1", instagramCarouselRequest("video/mp4", "video/mp4"), "ig1:d:-")
	if err == nil {
		t.Fatal("expected the rejection to surface")
	}
	if result.SubmissionState != PublishSubmissionRejected || result.RetrySafety != PublishRetrySafe || result.ProviderReference != "ig1:d:-" {
		t.Fatalf("a rejected resumed child create must be definite and retry-safe, got %#v", result)
	}
}
