package workflows

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	maxFollowChecks = 5
	// Meta's button reference limits a title to 20 characters. Instagram's
	// button template page states no limit of its own.
	maxButtonTitleRunes = 20
	// The button template's text, which is the first follow-up, in characters.
	maxButtonTextRunes = 640
	maxButtonURLBytes  = 2000
)

// FollowGate is the authored follow check of a comment campaign's follow-ups.
// The first follow-up becomes the text above the link button.
type FollowGate struct {
	ButtonTitle      string `json:"button_title"`
	ButtonURL        string `json:"button_url"`
	NotFollowingText string `json:"not_following_text"`
	MaxChecks        int    `json:"max_checks"`
}

// ParseFollowGate reads an optional follow_gate input. No value means no check.
func ParseFollowGate(value any) (*FollowGate, error) {
	if value == nil || value == "" {
		return nil, nil
	}
	raw, isText := value.(string)
	if !isText {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, invalid("the follow check must be an object")
		}
		raw = string(encoded)
	}
	if strings.TrimSpace(raw) == "null" {
		return nil, nil
	}
	var gate FollowGate
	if err := json.Unmarshal([]byte(raw), &gate); err != nil {
		return nil, invalid("the follow check needs a button title, an https link, a message and a whole number of checks")
	}
	if gate.MaxChecks == 0 {
		gate.MaxChecks = 3
	}
	title := strings.TrimSpace(gate.ButtonTitle)
	switch {
	case title == "":
		return nil, invalid("the link button needs a title")
	case utf8.RuneCountInString(title) > maxButtonTitleRunes:
		return nil, invalid(fmt.Sprintf("the link button title supports up to %d characters", maxButtonTitleRunes))
	}
	if err := validateButtonURL(gate.ButtonURL); err != nil {
		return nil, err
	}
	text := strings.TrimSpace(gate.NotFollowingText)
	switch {
	case text == "":
		return nil, invalid("write the message for people who do not follow the account yet")
	case len(text) > maxMessageBytes && !strings.Contains(text, "{{"):
		return nil, invalid(fmt.Sprintf("the message for people who do not follow exceeds Instagram's %d byte limit", maxMessageBytes))
	}
	if gate.MaxChecks < 1 || gate.MaxChecks > maxFollowChecks {
		return nil, invalid(fmt.Sprintf("check whether the person follows between 1 and %d times", maxFollowChecks))
	}
	return &gate, nil
}

func validateButtonURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "{{") {
		return invalid("enter the link button URL directly")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || len(raw) > maxButtonURLBytes {
		return invalid("the link button needs an https URL")
	}
	return nil
}

// validateFollowGate binds a follow check to the only place it can work: a
// comment campaign, where the person's reply gives Instagram's consent to share
// their profile, and a first follow-up that fits the button template.
func validateFollowGate(step Step, source Source, messages []SequenceMessage) error {
	gate, err := ParseFollowGate(step.Inputs["follow_gate"].Literal)
	if err != nil || gate == nil {
		return err
	}
	if source.Kind != "comment_received" {
		return invalid(step.Name + ": a follow check needs the comment trigger and a private reply")
	}
	if first := strings.TrimSpace(messages[0].Text); utf8.RuneCountInString(first) > maxButtonTextRunes && !strings.Contains(first, "{{") {
		return invalid(fmt.Sprintf("%s: the first follow-up carries the link button and supports up to %d characters", step.Name, maxButtonTextRunes))
	}
	return nil
}
