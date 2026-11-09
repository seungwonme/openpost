package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// followGateProvider records every Send API request and answers profile reads
// in place of the Instagram adapter, whose wire format the platform tests own.
type followGateProvider struct {
	automationProvider
	requests *[]platform.SendMessageRequest
	profiles *[]string
	profile  *func() (platform.MessagingUserProfile, error)
	polled   *[]platform.ProviderConversation
}

func (p followGateProvider) SendMessage(ctx context.Context, token string, input platform.SendMessageRequest) (platform.SendMessageResult, error) {
	*p.requests = append(*p.requests, input)
	result, err := p.automationProvider.SendMessage(ctx, token, input)
	// Every accepted send has its own provider ID, as Instagram's do.
	result.RemoteMessageID = fmt.Sprintf("%s-%d", result.RemoteMessageID, len(*p.requests))
	return result, err
}

func (p followGateProvider) FetchMessagingUserProfile(_ context.Context, _ string, userID string) (platform.MessagingUserProfile, error) {
	*p.profiles = append(*p.profiles, userID)
	return (*p.profile)()
}

func (p followGateProvider) FetchMessages(context.Context, string, platform.FetchMessagesRequest) (platform.FetchMessagesResult, error) {
	return platform.FetchMessagesResult{Conversations: *p.polled}, nil
}

type followGateFixture struct {
	automationFixture
	in       SequenceRequest
	requests *[]platform.SendMessageRequest
	profiles *[]string
	follows  *func() (platform.MessagingUserProfile, error)
	polled   *[]platform.ProviderConversation
}

func newFollowGateFixture(t *testing.T) followGateFixture {
	t.Helper()
	f := newAutomationFixture(t)
	base, ok := f.s.provider(f.account).(automationProvider)
	require.True(t, ok)
	requests, profiles, polled := []platform.SendMessageRequest{}, []string{}, []platform.ProviderConversation{}
	follows := func() (platform.MessagingUserProfile, error) {
		return platform.MessagingUserProfile{FollowsAccount: true}, nil
	}
	f.s.SetProvider("instagram", followGateProvider{automationProvider: base, requests: &requests, profiles: &profiles, profile: &follows, polled: &polled})
	in := f.commentSequenceRequest(t)
	in.Messages = []SequenceMessage{{Text: "Here is your guide"}, {Text: "Did it help?", DelayMinutes: 60}}
	in.FollowGate = &FollowGate{ButtonTitle: "Open guide", ButtonURL: "https://example.com/guide", NotFollowingText: "Follow us, then reply here again.", MaxChecks: 3}
	return followGateFixture{automationFixture: f, in: in, requests: &requests, profiles: &profiles, follows: &follows, polled: &polled}
}

func (f followGateFixture) setFollowing(following bool) {
	*f.follows = func() (platform.MessagingUserProfile, error) {
		return platform.MessagingUserProfile{FollowsAccount: following}, nil
	}
}

// poll stores a reply the way production receives it while the Meta app has no
// webhooks: the messages sync job reads the inbox and stores what is new.
func (f followGateFixture) poll(t *testing.T, id string, at time.Time) {
	t.Helper()
	*f.polled = []platform.ProviderConversation{{
		ID: "remote-thread", CounterpartRemoteID: "reader", LastMessageAt: at, LastRemoteMessageID: id, ReplyWindowExpiresAt: at.Add(24 * time.Hour),
		Messages: []platform.ProviderMessage{{ID: id, Direction: "inbound", AuthorRemoteID: "reader", Body: "done", RemoteCreatedAt: at}},
	}}
	require.NoError(t, f.s.HandleJob(t.Context(), JobTypeMessagesSync, `{"id":"account"}`))
	stored, err := f.db.NewSelect().Model((*models.DirectMessage)(nil)).Where("remote_message_id = ?", id).Exists(t.Context())
	require.NoError(t, err)
	require.True(t, stored, "the polled reply is stored")
}

func (f followGateFixture) checks(t *testing.T) []followCheck {
	t.Helper()
	var checks []followCheck
	require.NoError(t, f.db.NewSelect().Model(&checks).Order("attempt ASC").Scan(t.Context()))
	return checks
}

// askToFollow answers one polled reply from someone who does not follow.
func (f followGateFixture) askToFollow(t *testing.T, id string, at time.Time) SequenceProgress {
	t.Helper()
	f.setFollowing(false)
	f.poll(t, id, at)
	asking := f.advanceSequence(t, f.in)
	require.Equal(t, AutomationSending, asking.State)
	require.NoError(t, f.runSendJob(t, asking.Reply.MessageID))
	return f.advanceSequence(t, f.in)
}

func TestFollowGateSendsTheLinkButtonToAFollower(t *testing.T) {
	f := newFollowGateFixture(t)
	f.poll(t, "mid.response", f.now.Add(-time.Minute))

	sending := f.advanceSequence(t, f.in)
	require.Equal(t, AutomationSending, sending.State)
	assert.Equal(t, []string{"reader"}, *f.profiles, "the profile read uses the sender's messaging ID")
	require.NoError(t, f.runSendJob(t, sending.Reply.MessageID))

	require.Len(t, *f.requests, 1)
	assert.Equal(t, "Here is your guide", (*f.requests)[0].Body)
	assert.Equal(t, []platform.MessageButton{{Title: "Open guide", URL: "https://example.com/guide"}}, (*f.requests)[0].Buttons)
	var attachments []platform.MessageAttachment
	require.NoError(t, json.Unmarshal([]byte(f.message(t, sending.Reply.MessageID).AttachmentsJSON), &attachments))
	assert.Equal(t, []platform.MessageAttachment{{Type: "link_button", URL: "https://example.com/guide", Name: "Open guide"}}, attachments, "Inbox shows the button that was sent")

	next := f.advanceSequence(t, f.in)
	assert.Equal(t, AutomationScheduled, next.State, "the rest of the sequence follows the link")
	assert.Len(t, *f.profiles, 1, "a recorded check is never read again")
	*f.now = f.now.Add(time.Hour)
	later := f.advanceSequence(t, f.in)
	require.Equal(t, AutomationSending, later.State)
	require.NoError(t, f.runSendJob(t, later.Reply.MessageID))
	require.Len(t, *f.requests, 2)
	assert.Empty(t, (*f.requests)[1].Buttons, "only the first follow-up carries the button")
}

func TestFollowGateAsksANonFollowerAndChecksTheirNextReply(t *testing.T) {
	f := newFollowGateFixture(t)
	waiting := f.askToFollow(t, "mid.first", f.now.Add(-time.Minute))

	require.Len(t, *f.requests, 1)
	assert.Equal(t, "Follow us, then reply here again.", (*f.requests)[0].Body)
	assert.Empty(t, (*f.requests)[0].Buttons, "nobody who does not follow receives the link")
	assert.Equal(t, SequenceWaitingResponse, waiting.State)
	assert.Contains(t, waiting.Reason, "check 2 of 3")
	var wait struct{ RepliedAt time.Time }
	require.NoError(t, f.db.NewRaw(`SELECT replied_at FROM message_sequence_waits WHERE run_id = 'run'`).Scan(t.Context(), &wait))
	assert.True(t, wait.RepliedAt.Equal(*f.now), "the batched wake looks for a reply after the request to follow")
	assert.Equal(t, SequenceWaitingResponse, f.advanceSequence(t, f.in).State, "the same reply is never checked twice")
	assert.Len(t, *f.profiles, 1)

	*f.now = f.now.Add(10 * time.Minute)
	f.setFollowing(true)
	f.poll(t, "mid.second", *f.now)
	link := f.advanceSequence(t, f.in)
	require.Equal(t, AutomationSending, link.State)
	require.NoError(t, f.runSendJob(t, link.Reply.MessageID))
	require.Len(t, *f.requests, 2)
	assert.Equal(t, []platform.MessageButton{{Title: "Open guide", URL: "https://example.com/guide"}}, (*f.requests)[1].Buttons)
	assert.Equal(t, f.message(t, link.Reply.TriggerMessageID).RemoteMessageID, "mid.second", "the link answers the reply that passed the check")
	checks := f.checks(t)
	require.Len(t, checks, 2)
	assert.Equal(t, []string{FollowCheckNotFollowing, FollowCheckFollowing}, []string{checks[0].Outcome, checks[1].Outcome})
}

func TestFollowGateStopsAfterItsLastCheck(t *testing.T) {
	f := newFollowGateFixture(t)
	f.askToFollow(t, "mid.first", f.now.Add(-time.Minute))
	*f.now = f.now.Add(10 * time.Minute)
	f.askToFollow(t, "mid.second", f.now.Add(-time.Minute))
	*f.now = f.now.Add(10 * time.Minute)
	f.poll(t, "mid.third", f.now.Add(-time.Minute))

	stopped := f.advanceSequence(t, f.in)
	assert.Equal(t, SequenceNotFollowing, stopped.State)
	assert.Equal(t, stopReasonNotFollowing(3), stopped.Reason)
	assert.Len(t, *f.requests, 2, "two requests to follow, and no link")
	assert.Len(t, *f.profiles, 3)
	assert.Equal(t, SequenceNotFollowing, f.advanceSequence(t, f.in).State)
	assert.Len(t, *f.profiles, 3, "the stopped gate reads nothing more")
	parked, err := f.db.NewSelect().Table("message_sequence_waits").Where("run_id = 'run'").Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, parked)
}

func TestFollowGateShowsAFailedProfileRead(t *testing.T) {
	f := newFollowGateFixture(t)
	*f.follows = func() (platform.MessagingUserProfile, error) {
		return platform.MessagingUserProfile{}, platform.NewHTTPError(http.StatusBadRequest, http.Header{}, []byte(`{"error":{"message":"User consent is required to access user profile.","type":"IGApiException","code":230}}`))
	}
	f.poll(t, "mid.response", f.now.Add(-time.Minute))

	stopped := f.advanceSequence(t, f.in)
	assert.Equal(t, SequenceStopped, stopped.State, "a failed read is not \"not following\"")
	assert.Contains(t, stopped.Reason, "could not read whether this person follows")
	assert.Contains(t, stopped.Reason, "HTTP 400")
	assert.Empty(t, *f.requests, "neither the link nor a request to follow is sent")
	assert.Equal(t, SequenceStopped, f.advanceSequence(t, f.in).State)
	assert.Len(t, *f.profiles, 1, "the failure is recorded once")
	checks := f.checks(t)
	require.Len(t, checks, 1)
	assert.Equal(t, FollowCheckFailed, checks[0].Outcome)
}

func TestFollowGateWaitsForTheFirstResponse(t *testing.T) {
	f := newFollowGateFixture(t)
	waiting := f.advanceSequence(t, f.in)
	assert.Equal(t, SequenceWaitingResponse, waiting.State)
	assert.Empty(t, *f.profiles, "a comment alone gives no consent to read the profile")
	assert.Empty(t, f.checks(t))
}

func TestFollowGateEndsWhenTheNextReplyNeverComes(t *testing.T) {
	f := newFollowGateFixture(t)
	f.askToFollow(t, "mid.first", f.now.Add(-time.Minute))
	*f.now = f.now.Add(24 * time.Hour)
	ended := f.advanceSequence(t, f.in)
	assert.Equal(t, SequenceNoResponse, ended.State)
	assert.Equal(t, stopReasonNoFollowReply, ended.Reason)
	assert.Len(t, *f.requests, 1)
}

func TestFollowGateRejectsLinkTextOverTheTemplateLimit(t *testing.T) {
	f := newFollowGateFixture(t)
	f.in.Messages[0].Text = strings.Repeat("a", platform.MessageButtonTextLimit+1)
	f.poll(t, "mid.response", f.now.Add(-time.Minute))
	stopped := f.advanceSequence(t, f.in)
	assert.Equal(t, SequenceStopped, stopped.State)
	assert.Contains(t, stopped.Reason, "640 characters")
	assert.Empty(t, *f.requests)
}
