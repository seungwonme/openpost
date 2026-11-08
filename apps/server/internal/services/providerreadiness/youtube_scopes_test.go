package providerreadiness

import (
	"slices"
	"testing"

	"github.com/openpost/backend/internal/capabilities"
)

func TestYouTubePublishRequiresOnlyUploadScope(t *testing.T) {
	const upload = "https://www.googleapis.com/auth/youtube.upload"
	required := RequiredScopesForSubject(Subject{Provider: capabilities.ProviderYouTube, Operation: OperationPublishImmediate})
	if !slices.Equal(required, []string{upload}) {
		t.Fatalf("YouTube publish scopes = %v, want only %s", required, upload)
	}
	grants := map[string][]string{
		"minimal grant": {upload, "https://www.googleapis.com/auth/youtube.force-ssl", "https://www.googleapis.com/auth/yt-analytics.readonly"},
		"legacy superset": {
			"https://www.googleapis.com/auth/youtube", "https://www.googleapis.com/auth/youtube.readonly", upload,
			"https://www.googleapis.com/auth/yt-analytics.readonly",
		},
	}
	for name, granted := range grants {
		if !containsAll(granted, required) {
			t.Errorf("%s must satisfy YouTube publish readiness", name)
		}
	}
	if containsAll([]string{"https://www.googleapis.com/auth/youtube.force-ssl"}, required) {
		t.Error("a grant without youtube.upload must not satisfy YouTube publish readiness")
	}
}
