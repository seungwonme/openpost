package publisher

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/stretchr/testify/require"
)

// directPendingAdapter models a provider that takes the file inside its publish
// write and finishes asynchronously, like TikTok's Upload/Inbox video.
type directPendingAdapter struct {
	*fakePublisherAdapter
	direct, checks int
	bodies         []string
	published      bool
	selected       bool
}

func (d *directPendingAdapter) PublishesMediaDirectly(map[string]interface{}, []string) bool {
	return d.selected
}

func (d *directPendingAdapter) PublishWithMedia(_ context.Context, _, _ string, req *platform.PublishRequest, media []platform.UploadMediaRequest) (platform.PublishResult, error) {
	d.direct++
	body, err := io.ReadAll(media[0].Reader)
	if err != nil {
		return platform.PublishResult{}, err
	}
	d.bodies = append(d.bodies, string(body))
	result := platform.PublishResult{SubmissionState: platform.PublishSubmissionPending, ProviderState: "processing", ProviderReference: "publish-1", RetrySafety: platform.PublishRetryReconcileOnly, ReconcileAfter: time.Minute}
	return result, req.BeginWrite(result)
}

func (d *directPendingAdapter) ReconcilePublish(_ context.Context, _, _, reference string) (platform.PublishResult, error) {
	d.checks++
	if d.published {
		return platform.AcceptedPublishResult("external-1"), nil
	}
	return platform.PublishResult{SubmissionState: platform.PublishSubmissionPending, ProviderState: "processing", ProviderReference: reference, RetrySafety: platform.PublishRetryReconcileOnly, ReconcileAfter: time.Minute}, nil
}

func TestDirectMediaPendingWriteReconcilesWithoutUploadingAgain(t *testing.T) {
	base := &fakePublisherAdapter{}
	srv := newPublisherLifecycleTestServer(t, base)
	adapter := &directPendingAdapter{fakePublisherAdapter: base, selected: true}
	srv.service.SetProvider("x", adapter)
	srv.service.SetStorage(&fakePublisherStorage{body: "fixture-video"})
	ctx := t.Context()
	for _, model := range []any{(*models.RenditionMediaDelivery)(nil), (*models.RenditionMediaDeliveryRelation)(nil), (*models.PublicationSegmentMedia)(nil)} {
		_, err := srv.db.NewCreateTable().Model(model).IfNotExists().Exec(ctx)
		require.NoError(t, err)
	}
	_, err := srv.db.NewInsert().Model(&models.MediaAttachment{
		ID: "video", WorkspaceID: "ws-1", OriginalFilename: "video.mp4", FilePath: "media/video.mp4",
		MimeType: "video/mp4", Size: 13, ProcessingStatus: "ready", AnalysisStatus: "ready",
	}).Exec(ctx)
	require.NoError(t, err)
	_, err = srv.db.NewInsert().Model(&models.RenditionMedia{RenditionID: "rendition-1", MediaID: "video", Role: "attachment"}).Exec(ctx)
	require.NoError(t, err)
	jobCtx, payload := srv.authorizedPublicationJob(t)

	for attempt := 0; attempt < 3; attempt++ {
		err := srv.service.HandlePublishPublicationJob(jobCtx, payload)
		var continuation *RetryableError
		require.ErrorAs(t, err, &continuation, "a processing upload must continue, not fail")
		require.Equal(t, "provider_submission_pending", continuation.Failure.Code)
		var rendition models.Rendition
		require.NoError(t, srv.db.NewSelect().Model(&rendition).Where("id = ?", "rendition-1").Scan(jobCtx))
		require.Equal(t, models.RenditionStatusPublishing, rendition.Status)
		_, err = srv.db.NewUpdate().Model((*models.ProviderWriteAttempt)(nil)).
			Set("reconcile_after = ?", time.Now().UTC().Add(-time.Minute)).Where("publication_id = ?", "publication-1").Exec(jobCtx)
		require.NoError(t, err)
	}
	adapter.published = true
	require.NoError(t, srv.service.HandlePublishPublicationJob(jobCtx, payload))

	require.Equal(t, 1, adapter.direct, "retries must reconcile the same write, not upload the file again")
	require.Equal(t, []string{"fixture-video"}, adapter.bodies)
	require.Zero(t, adapter.uploadCalls, "the file must not be uploaded in the media step")
	require.Zero(t, adapter.publishCalls, "the provider-media Publish path must not run")
	var rendition models.Rendition
	require.NoError(t, srv.db.NewSelect().Model(&rendition).Where("id = ?", "rendition-1").Scan(jobCtx))
	require.Equal(t, models.RenditionStatusPublished, rendition.Status)
	require.Equal(t, "external-1", rendition.ExternalID)
}

func TestDirectMediaSelectorFalseKeepsProviderMediaPath(t *testing.T) {
	adapter := &directPendingAdapter{fakePublisherAdapter: &fakePublisherAdapter{}, selected: false}
	require.False(t, publishesMediaDirectly(adapter, nil, []models.MediaAttachment{{MimeType: "image/jpeg"}}))
	adapter.selected = true
	require.True(t, publishesMediaDirectly(adapter, nil, []models.MediaAttachment{{MimeType: "video/mp4"}}))
	require.False(t, publishesMediaDirectly(&fakePublisherAdapter{}, nil, nil), "a provider without direct media keeps the provider-media path")
}
