package workflows

import (
	"context"
	"time"

	"github.com/openpost/backend/internal/jobregistry"
	"github.com/openpost/backend/internal/models"
	"github.com/uptrace/bun"
)

// pullRunJobForward makes a run's pending job due no later than runAt.
//
// A run woken before its timer (wakeRespondedRuns: a response, an opt-out or a
// takeover) keeps its revision, so it keeps the job its wait scheduled for the
// end of the response window. The active dedupe identity then turns every
// later enqueue for that revision into a no-op, and the woken run would sit
// until the far-future job came due. Moving that job is the only way to make it
// run without a second active job for the same revision.
//
// Only a job no worker has acquired moves. lease_generation 0 means its run_at
// is still the schedule chosen when it was queued, not a retry backoff, and a
// processing job is never touched. executeRun still claims the run only when
// wake_at has passed, so an early job cannot run a step early.
func pullRunJobForward(ctx context.Context, db bun.IDB, scopeID, dedupeKey string, runAt time.Time) error {
	_, err := db.NewUpdate().Model((*models.Job)(nil)).Set("run_at = ?", runAt.UTC()).
		Where("type = ? AND scope_id = ? AND dedupe_key = ?", jobregistry.TypeWorkflowRun, scopeID, dedupeKey).
		Where("status = ? AND lease_generation = 0 AND run_at > ?", jobregistry.StatusPending, runAt.UTC()).
		Exec(ctx)
	return err
}
