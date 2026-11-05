package workflows

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// KindMessageSequence sends ordered follow-ups after a person's response.
const KindMessageSequence = "message_sequence"

const (
	maxSequenceMessages = 5
	// How long a comment campaign waits for a response before it stops. The
	// provider sets no such limit; this keeps a run from waiting indefinitely.
	maxResponseWindowMinutes = 7 * 24 * 60
	maxMessageBytes          = 1000
)

// SequenceMessage is one authored follow-up.
type SequenceMessage struct {
	Text         string `json:"text"`
	DelayMinutes int    `json:"delay_minutes"`
}

// A sequence reads each position's text from its receipt, so one review must not
// leave text behind for the next one.
func keepReviewedText(step *StepResult, effect EffectResult) {
	if step.Kind != KindMessageSequence {
		step.Inputs["text"] = effect.Output["text"]
	}
}

// ParseMessageSequence reads the ordered messages of a message_sequence step.
func ParseMessageSequence(value any) ([]SequenceMessage, error) {
	raw, isText := value.(string)
	if !isText {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, invalid("follow-up messages must be a list")
		}
		raw = string(encoded)
	}
	var messages []SequenceMessage
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		return nil, invalid("follow-up messages must be a list of text and whole-minute delays")
	}
	if len(messages) == 0 || len(messages) > maxSequenceMessages {
		return nil, invalid(fmt.Sprintf("add between 1 and %d follow-up messages", maxSequenceMessages))
	}
	for i, message := range messages {
		text := strings.TrimSpace(message.Text)
		if text == "" {
			return nil, invalid(fmt.Sprintf("follow-up message %d needs text", i+1))
		}
		if len(text) > maxMessageBytes && !strings.Contains(text, "{{") {
			return nil, invalid(fmt.Sprintf("follow-up message %d exceeds Instagram's %d byte limit", i+1, maxMessageBytes))
		}
		if message.DelayMinutes < 0 || message.DelayMinutes >= messageWindowMinutes {
			return nil, invalid(fmt.Sprintf("follow-up message %d needs a delay under 24 hours", i+1))
		}
	}
	return messages, nil
}

// MessageResponseWindow is how long a comment campaign waits for a response.
func MessageResponseWindow(value any) (time.Duration, error) {
	if value == nil || value == "" {
		return maxResponseWindowMinutes * time.Minute, nil
	}
	minutes, err := number(value)
	if err != nil || minutes < 1 || minutes > maxResponseWindowMinutes {
		return 0, invalid("the response window must be between 1 minute and 7 days")
	}
	return time.Duration(minutes * float64(time.Minute)), nil
}

func validateMessageSequenceStep(step Step) error {
	for name := range step.Inputs {
		if !slices.Contains([]string{"engagement_id", "message_id", "messages", "delivery", "response_window_minutes"}, name) {
			return invalid("unsupported follow-up field: " + name)
		}
	}
	for _, name := range []string{"messages", "delivery", "response_window_minutes"} {
		if step.Inputs[name].Reference != "" {
			return invalid("choose follow-up " + strings.ReplaceAll(name, "_", " ") + " directly")
		}
	}
	if !slices.Contains([]string{"review", "automatic"}, fmt.Sprint(step.Inputs["delivery"].Literal)) {
		return invalid("choose review or automatic delivery")
	}
	if _, err := ParseMessageSequence(step.Inputs["messages"].Literal); err != nil {
		return err
	}
	_, err := MessageResponseWindow(step.Inputs["response_window_minutes"].Literal)
	return err
}

// validateSequence binds follow-ups to the only people they can reach: someone
// whose own message started this run, or who answered this run's private reply.
func (w messageStepWalker) validateSequence(step Step, path messagePath) error {
	messages, err := ParseMessageSequence(step.Inputs["messages"].Literal)
	if err != nil {
		return err
	}
	delay := 0.0
	for _, message := range messages {
		delay += float64(message.DelayMinutes)
	}
	switch w.source.Kind {
	case "message_received":
		if step.Inputs["message_id"].Reference != "source.message_id" || step.Inputs["engagement_id"] != (Value{}) {
			return invalid(step.Name + ": follow-ups must answer the message that started this run")
		}
		delay += path.elapsed
	case "comment_received":
		if step.Inputs["engagement_id"].Reference != "source.engagement_id" || step.Inputs["message_id"] != (Value{}) {
			return invalid(step.Name + ": follow-ups must continue the comment that started this run")
		}
		if !path.private {
			return invalid(step.Name + ": follow-ups need a private reply earlier in this path")
		}
	default:
		return invalid(step.Name + ": follow-ups need a comment or incoming message trigger")
	}
	if delay >= messageWindowMinutes {
		return invalid(step.Name + ": these delays outlast Instagram's 24 hour messaging window")
	}
	return nil
}
