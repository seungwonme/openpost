package workflows

import (
	"strings"
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageFollowGateValidation(t *testing.T) {
	comment := Source{Kind: "comment_received", AccountIDs: []string{"ig"}, Keywords: []string{"guide"}, MatchMode: "word", DailyLimit: 20}
	private := Step{ID: "private", Kind: KindPrivateReply, Name: "Private", Inputs: map[string]Value{"engagement_id": reference("source.engagement_id"), "text": literal("Reply to get the link"), "delivery": literal("review")}}
	two := `[{"text":"Here is your guide","delay_minutes":0},{"text":"Did it help?","delay_minutes":60}]`
	gate := func(fields map[string]any) map[string]Value {
		value := map[string]any{"button_title": "Open guide", "button_url": "https://example.com/guide", "not_following_text": "Follow us, then reply again.", "max_checks": float64(3)}
		for name, field := range fields {
			value[name] = field
		}
		return map[string]Value{"engagement_id": reference("source.engagement_id"), "follow_gate": literal(value)}
	}
	fromComment := func(fields map[string]any) []Step {
		return []Step{private, sequenceStep(two, gate(fields))}
	}
	cases := []struct {
		name   string
		source Source
		steps  []Step
		want   string
	}{
		{"a comment campaign can gate its link", comment, fromComment(nil), ""},
		{"stored as JSON text", comment, []Step{private, sequenceStep(two, map[string]Value{"engagement_id": reference("source.engagement_id"), "follow_gate": literal(`{"button_title":"Open","button_url":"https://example.com","not_following_text":"Follow first"}`)})}, ""},
		{"off when empty", comment, []Step{private, sequenceStep(two, map[string]Value{"engagement_id": reference("source.engagement_id"), "follow_gate": literal(nil)})}, ""},
		{"a message trigger has no comment to gate", messageSource(), []Step{sequenceStep(two, map[string]Value{"message_id": reference("source.message_id"), "follow_gate": gate(nil)["follow_gate"]})}, "comment trigger"},
		{"the link must use https", comment, fromComment(map[string]any{"button_url": "http://example.com/guide"}), "https URL"},
		{"the link is not a variable", comment, fromComment(map[string]any{"button_url": "https://{{source.text}}"}), "directly"},
		{"the button needs a title", comment, fromComment(map[string]any{"button_title": " "}), "needs a title"},
		{"the button title is short", comment, fromComment(map[string]any{"button_title": strings.Repeat("a", 21)}), "20 characters"},
		{"people who do not follow get a message", comment, fromComment(map[string]any{"not_following_text": ""}), "do not follow"},
		{"checks are bounded", comment, fromComment(map[string]any{"max_checks": float64(6)}), "between 1 and 5"},
		{"checks are whole", comment, fromComment(map[string]any{"max_checks": 1.5}), "whole number"},
		{"the first follow-up fits the template", comment, []Step{private, sequenceStep([]any{map[string]any{"text": strings.Repeat("a", 641)}}, gate(nil))}, "640 characters"},
		{"the follow check is chosen directly", comment, []Step{private, sequenceStep(two, map[string]Value{"engagement_id": reference("source.engagement_id"), "follow_gate": reference("source.text")})}, "follow gate directly"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := Validate(Definition{Schema: 1, Source: test.source, Steps: test.steps}, true)
			if test.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.want)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestBatchedCheckWakesAFollowCheckOnlyForTheQuickReplyTitle(t *testing.T) {
	eachMessageBackend(t, func(t *testing.T, f *messageSourceFixture) {
		workflow := f.publish(t, messageSource(), nil)
		far := f.now.Add(7 * 24 * time.Hour).Truncate(time.Second)
		f.parkedRun(t, workflow.ID, far)
		_, err := f.s.db.NewRaw(`UPDATE message_sequence_waits SET response_text = '팔로우했어요' WHERE run_id = 'parked'`).Exec(t.Context())
		require.NoError(t, err)

		f.message(t, models.DirectMessage{ID: "chat", AuthorRemoteID: "reader", Body: "what is this?", RemoteCreatedAt: f.now.Add(time.Minute)})
		require.NoError(t, f.s.wakeRespondedRuns(t.Context(), f.now.Add(2*time.Minute)))
		assert.WithinDuration(t, far, f.parkedWake(t), time.Second, "other text does not wake the run")

		f.message(t, models.DirectMessage{ID: "tap", AuthorRemoteID: "reader", Body: "팔로우했어요", RemoteCreatedAt: f.now.Add(2 * time.Minute)})
		wakeAt := f.now.Add(3 * time.Minute)
		require.NoError(t, f.s.wakeRespondedRuns(t.Context(), wakeAt))
		assert.WithinDuration(t, wakeAt, f.parkedWake(t), time.Second)
	})
}

func TestQuickReplyTitleValidation(t *testing.T) {
	comment := Source{Kind: "comment_received", AccountIDs: []string{"ig"}, Keywords: []string{"guide"}, MatchMode: "word", DailyLimit: 20}
	private := func(kind string, inputs map[string]Value) []Step {
		step := Step{ID: "private", Kind: kind, Name: "Private", Inputs: map[string]Value{"engagement_id": reference("source.engagement_id"), "text": literal("Tap the button once you follow us"), "delivery": literal("review")}}
		for name, value := range inputs {
			step.Inputs[name] = value
		}
		return []Step{step}
	}
	cases := []struct {
		name  string
		steps []Step
		want  string
	}{
		{"a private reply can offer a quick reply", private(KindPrivateReply, map[string]Value{"quick_reply_title": literal("팔로우했어요")}), ""},
		{"a private channel comment reply can too", private(KindCommentReply, map[string]Value{"channel": literal("private"), "quick_reply_title": literal("팔로우했어요")}), ""},
		{"empty means no quick reply", private(KindPrivateReply, map[string]Value{"quick_reply_title": literal("")}), ""},
		{"a public reply cannot", private(KindCommentReply, map[string]Value{"channel": literal("public"), "quick_reply_title": literal("팔로우했어요")}), "only a private reply"},
		{"Instagram truncates after 20 characters", private(KindPrivateReply, map[string]Value{"quick_reply_title": literal(strings.Repeat("가", 21))}), "20 characters"},
		{"the title is entered directly", private(KindPrivateReply, map[string]Value{"quick_reply_title": reference("source.text")}), "directly"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := Validate(Definition{Schema: 1, Source: comment, Steps: test.steps}, true)
			if test.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.want)
		})
	}
}
