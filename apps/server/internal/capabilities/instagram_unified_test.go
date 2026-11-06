package capabilities

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstagramUnifiedSettingsBlockOnlyLocation(t *testing.T) {
	capability := Capability{Provider: ProviderInstagram, Profile: "image_post"}

	issues := InstagramUnifiedSettingsIssues(capability, map[string]any{"location_id": "123"})
	require.Len(t, issues, 1)
	require.Equal(t, "location_id", issues[0].Field)
	require.Contains(t, issues[0].Message, "through a Facebook Page")

	require.Empty(t, InstagramUnifiedSettingsIssues(capability, map[string]any{"product_tags": `[{"product_id":"1"}]`}))
	require.NotEmpty(t, InstagramLoginSettingsIssues(capability, map[string]any{"product_tags": `[{"product_id":"1"}]`}),
		"direct login keeps its own restriction")
}

func TestApplyInstagramUnifiedSettingsMarksTheLocationField(t *testing.T) {
	resolved := &ResolvedCapability{
		Capability: Capability{
			Provider: ProviderInstagram, Profile: "image_post",
			Settings: []SettingDefinition{{Key: "location_id"}, {Key: "collaborators"}},
		},
		Compatible: true,
	}
	ApplyInstagramUnifiedSettings(resolved, map[string]any{})
	require.True(t, resolved.Compatible)
	require.NotEmpty(t, resolved.Settings[0].UnavailableReason)
	require.Empty(t, resolved.Settings[1].UnavailableReason)

	ApplyInstagramUnifiedSettings(resolved, map[string]any{"location_id": "123"})
	require.False(t, resolved.Compatible)
}
