package engagement

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/stretchr/testify/require"
)

type quickReplyCampaignProvider struct {
	*campaignProvider
	quickReplies [][]platform.QuickReply
	reject       error
}

func (p *quickReplyCampaignProvider) ReplyPrivatelyToCommentWithQuickReplies(_ context.Context, _, _, _, _ string, quickReplies []platform.QuickReply) (string, error) {
	p.quickReplies = append(p.quickReplies, quickReplies)
	if p.reject != nil {
		return "", p.reject
	}
	return "dm-quick", nil
}

func quickReplyCampaign(t *testing.T) (*Service, AutomationReplyRequest, *quickReplyCampaignProvider) {
	t.Helper()
	s, in, base := campaignTest(t)
	provider := &quickReplyCampaignProvider{campaignProvider: base}
	s.SetProvider("instagram", provider)
	in.QuickReplies = []platform.QuickReply{{Title: "팔로우했어요", Payload: platform.FollowCheckQuickReplyPayload}}
	return s, in, provider
}

func TestPrivateReplyCarriesItsQuickReplyToTheProvider(t *testing.T) {
	s, in, p := quickReplyCampaign(t)
	_, err := s.db.NewRaw(`UPDATE workflow_runs SET definition_json=? WHERE id='run'`, `{"schema":1,"steps":[{"id":"send","kind":"private_comment_reply","name":"Ask","inputs":{"quick_reply_title":{"literal":" 팔로우했어요 "}}}]}`).Exec(t.Context())
	require.NoError(t, err)
	reply, err := s.AdvanceAutomationReply(t.Context(), in)
	require.NoError(t, err)
	var job models.Job
	require.NoError(t, s.db.NewSelect().Model(&job).Where("id=?", reply.JobID).Scan(t.Context()))
	var payload engagementActionJob
	require.NoError(t, json.Unmarshal([]byte(job.Payload), &payload))
	require.Equal(t, in.QuickReplies, payload.QuickReplies, "the durable job carries the quick reply")

	completeCampaignJob(t, s, reply.JobID)
	require.Equal(t, [][]platform.QuickReply{in.QuickReplies}, p.quickReplies)
	require.Zero(t, p.private, "a quick reply never goes out as a plain private reply")
	reply, err = s.AdvanceAutomationReply(t.Context(), in)
	require.NoError(t, err)
	require.Equal(t, "sent", reply.State)
	delivered, err := s.RunPrivateReply(t.Context(), in.Authority, "run", "c")
	require.NoError(t, err)
	require.Equal(t, "팔로우했어요", delivered.QuickReplyTitle, "the follow check waits for the title it asked for")
}

func TestRejectedQuickReplyFailsWithoutAPlainResend(t *testing.T) {
	s, in, p := quickReplyCampaign(t)
	p.reject = platform.NewHTTPError(http.StatusBadRequest, http.Header{}, []byte(`{"error":{"message":"Invalid parameter","code":100}}`))
	reply, err := s.AdvanceAutomationReply(t.Context(), in)
	require.NoError(t, err)
	var job models.Job
	require.NoError(t, s.db.NewSelect().Model(&job).Where("id=?", reply.JobID).Scan(t.Context()))
	var payload engagementActionJob
	require.NoError(t, json.Unmarshal([]byte(job.Payload), &payload))
	err = s.performEngagementAction(t.Context(), payload)
	var providerErr *platform.HTTPError
	require.True(t, errors.As(err, &providerErr), "the rejection is reported: %v", err)
	require.Len(t, p.quickReplies, 1)
	require.Zero(t, p.private)
}
