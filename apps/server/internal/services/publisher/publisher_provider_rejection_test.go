package publisher

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/stretchr/testify/require"
)

// stagedRejectionAdapter models a staged provider write: it checkpoints a
// pending stage before the container call, as the Instagram carousel does,
// then answers the call with a definite 4xx rejection or a transient
// outage, depending on the scenario.
type stagedRejectionAdapter struct {
	*fakePublisherAdapter
	rejectOnPublish bool
	rejectOnResume  bool
	resumes         int
}

func stagedPending() platform.PublishResult {
	return platform.PublishResult{
		SubmissionState:   platform.PublishSubmissionPending,
		ProviderState:     "instagram:v1:carousel_child_create_started",
		ProviderReference: "ig1:d:-",
		RetrySafety:       platform.PublishRetryReconcileOnly,
		ReconcileAfter:    time.Minute,
	}
}

func stagedRejection() (platform.PublishResult, error) {
	rejected := stagedPending()
	rejected.SubmissionState = platform.PublishSubmissionRejected
	rejected.RetrySafety = platform.PublishRetrySafe
	return rejected, &platform.HTTPError{StatusCode: http.StatusBadRequest, Code: "meta:100"}
}

func (s *stagedRejectionAdapter) Publish(_ context.Context, _, _ string, req *platform.PublishRequest) (platform.PublishResult, error) {
	s.publishCalls++
	pending := stagedPending()
	if err := req.BeginWrite(pending); err != nil {
		return platform.PublishResult{}, err
	}
	if err := req.Checkpoint(pending); err != nil {
		return platform.PublishResult{}, err
	}
	if s.rejectOnPublish {
		return stagedRejection()
	}
	// The pre-fix adapter returned the bare error and left the pending
	// checkpoint to backfill the result.
	return platform.PublishResult{}, &platform.HTTPError{StatusCode: http.StatusBadRequest, Code: "meta:100"}
}

func (s *stagedRejectionAdapter) ResumePublish(_ context.Context, _, _ string, _ *platform.PublishRequest, _ string) (platform.PublishResult, error) {
	s.resumes++
	if s.rejectOnResume {
		return stagedRejection()
	}
	return stagedPending(), nil
}

func newStagedRejectionServer(t *testing.T, adapter *stagedRejectionAdapter) *publisherLifecycleTestServer {
	t.Helper()
	srv := newPublisherLifecycleTestServer(t, adapter.fakePublisherAdapter)
	srv.service.SetProvider("x", adapter)
	return srv
}

func requireRenditionFailed(t *testing.T, srv *publisherLifecycleTestServer) models.Rendition {
	t.Helper()
	var rendition models.Rendition
	require.NoError(t, srv.db.NewSelect().Model(&rendition).Where("id = ?", "rendition-1").Scan(t.Context()))
	require.Equal(t, models.RenditionStatusFailed, rendition.Status)
	require.Equal(t, FailureValidation, rendition.ErrorKind)
	require.Equal(t, "meta:100", rendition.ErrorCode, "the provider error code must be preserved")
	require.Equal(t, http.StatusBadRequest, rendition.ErrorHTTPStatus)
	var publication models.Publication
	require.NoError(t, srv.db.NewSelect().Model(&publication).Where("id = ?", "publication-1").Scan(t.Context()))
	require.Equal(t, models.PublicationStatusFailed, publication.Status)
	return rendition
}

func TestStagedProviderRejectionFailsRenditionInsteadOfPending(t *testing.T) {
	adapter := &stagedRejectionAdapter{fakePublisherAdapter: &fakePublisherAdapter{}, rejectOnPublish: true}
	srv := newStagedRejectionServer(t, adapter)

	err := srv.publishPublication(t)
	var continuation *RetryableError
	require.NotErrorAs(t, err, &continuation, "a definite rejection must not continue as pending")
	requireRenditionFailed(t, srv)

	var attempt models.ProviderWriteAttempt
	require.NoError(t, srv.db.NewSelect().Model(&attempt).Where("publication_id = ?", "publication-1").Scan(t.Context()))
	require.Equal(t, "definite_failure", attempt.Status)
	require.Equal(t, string(platform.PublishRetrySafe), attempt.RetrySafety, "the operator may retry a rejected container")
}

func TestStuckStagedPendingWriteFailsOnceResumeReportsRejection(t *testing.T) {
	adapter := &stagedRejectionAdapter{fakePublisherAdapter: &fakePublisherAdapter{}}
	srv := newStagedRejectionServer(t, adapter)
	ctx, payload := srv.authorizedPublicationJob(t)

	// Before the fix: the 400 left the write pending and every worker pass
	// resumed it without ever failing the destination.
	for pass := 0; pass < 3; pass++ {
		err := srv.service.HandlePublishPublicationJob(ctx, payload)
		var continuation *RetryableError
		require.ErrorAs(t, err, &continuation)
		require.Equal(t, "provider_submission_pending", continuation.Failure.Code)
		_, updateErr := srv.db.NewUpdate().Model((*models.ProviderWriteAttempt)(nil)).
			Set("reconcile_after = ?", time.Now().UTC().Add(-time.Minute)).Where("publication_id = ?", "publication-1").Exec(ctx)
		require.NoError(t, updateErr)
	}
	var rendition models.Rendition
	require.NoError(t, srv.db.NewSelect().Model(&rendition).Where("id = ?", "rendition-1").Scan(ctx))
	require.Equal(t, models.RenditionStatusPublishing, rendition.Status)

	// After deploy the same stored write resumes and the fixed adapter
	// reports the definite rejection.
	adapter.rejectOnResume = true
	err := srv.service.HandlePublishPublicationJob(ctx, payload)
	var continuation *RetryableError
	require.NotErrorAs(t, err, &continuation)
	requireRenditionFailed(t, srv)
	require.Equal(t, 1, adapter.publishCalls, "recovery must not send the write again")
	var attempt models.ProviderWriteAttempt
	require.NoError(t, srv.db.NewSelect().Model(&attempt).Where("publication_id = ?", "publication-1").Scan(ctx))
	require.Equal(t, string(platform.PublishRetrySafe), attempt.RetrySafety, "a rejected resumed creation stays retryable by the operator")
}
