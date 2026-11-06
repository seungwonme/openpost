package capabilities

import "github.com/openpost/backend/internal/providerlimits"

const (
	instagramLoginSettingReason   = "This setting requires an Instagram connection through Facebook."
	instagramUnifiedSettingReason = "This setting requires an Instagram connection through a Facebook Page."
)

func ApplyInstagramLoginSettings(resolved *ResolvedCapability, settings map[string]any) {
	applyInstagramSettingLimits(resolved, settings, providerlimits.InstagramLoginSettingUnavailable, instagramLoginSettingReason)
}

func InstagramLoginSettingsIssues(capability Capability, settings map[string]any) []ValidationIssue {
	return instagramSettingLimitIssues(capability, settings, providerlimits.InstagramLoginSettingUnavailable, instagramLoginSettingReason)
}

func ApplyInstagramUnifiedSettings(resolved *ResolvedCapability, settings map[string]any) {
	applyInstagramSettingLimits(resolved, settings, providerlimits.InstagramUnifiedSettingUnavailable, instagramUnifiedSettingReason)
}

func InstagramUnifiedSettingsIssues(capability Capability, settings map[string]any) []ValidationIssue {
	return instagramSettingLimitIssues(capability, settings, providerlimits.InstagramUnifiedSettingUnavailable, instagramUnifiedSettingReason)
}

func applyInstagramSettingLimits(resolved *ResolvedCapability, settings map[string]any, unavailable func(string) bool, reason string) {
	issues := instagramSettingLimitIssues(resolved.Capability, settings, unavailable, reason)
	resolved.Issues = append(resolved.Issues, issues...)
	if len(issues) > 0 {
		resolved.Compatible = false
	}
	for index := range resolved.Settings {
		field := &resolved.Settings[index]
		if unavailable(field.Key) {
			field.UnavailableReason = reason
		}
	}
	for groupIndex := range resolved.SettingGroups {
		for index := range resolved.SettingGroups[groupIndex].Settings {
			field := &resolved.SettingGroups[groupIndex].Settings[index]
			if unavailable(field.Key) {
				field.UnavailableReason = reason
			}
		}
	}
}

func instagramSettingLimitIssues(capability Capability, settings map[string]any, unavailable func(string) bool, reason string) []ValidationIssue {
	var issues []ValidationIssue
	for _, field := range instagramSettings(capability.Profile) {
		value := settingsValue(settings, field.Key)
		if unavailable(field.Key) && value != "" && value != "[]" && value != "<nil>" {
			issues = append(issues, settingValidationIssue(capability, field, "unsupported_setting", reason))
		}
	}
	return issues
}
