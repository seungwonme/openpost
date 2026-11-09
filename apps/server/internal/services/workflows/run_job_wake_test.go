package workflows

import (
	"testing"
	"time"

	"github.com/openpost/backend/internal/jobregistry"
	"github.com/openpost/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The sweep makes a woken run's scheduled job due, and leaves a job a worker
// already acquired alone: its run_at is a retry backoff, or it is running.
func TestSweepPullsOnlyANeverAcquiredJobOfADueRun(t *testing.T) {
	for name, test := range map[string]struct {
		job   models.Job
		moved bool
	}{
		"scheduled by the wait":    {job: models.Job{Status: jobregistry.StatusPending}, moved: true},
		"retrying after a failure": {job: models.Job{Status: jobregistry.StatusPending, Attempts: 1, LeaseGeneration: 1}},
		"running":                  {job: models.Job{Status: jobregistry.StatusProcessing, LeaseGeneration: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			eachMessageBackend(t, func(t *testing.T, f *messageSourceFixture) {
				workflow := f.publish(t, messageSource(), nil)
				f.parkedRun(t, workflow.ID, f.now.Add(-time.Minute))
				later := f.now.Add(7 * 24 * time.Hour).Truncate(time.Second)
				job := test.job
				job.ID, job.Type, job.ScopeID, job.DedupeKey, job.Payload, job.RunAt, job.MaxAttempts = "job", jobregistry.TypeWorkflowRun, "ws", "parked:1", `{"run_id":"parked"}`, later, 5
				_, err := f.s.db.NewInsert().Model(&job).Exec(t.Context())
				require.NoError(t, err)

				require.NoError(t, f.s.sweep(t.Context()))

				var jobs []models.Job
				require.NoError(t, f.s.db.NewSelect().Model(&jobs).Where("type = ?", jobregistry.TypeWorkflowRun).Scan(t.Context()))
				require.Len(t, jobs, 1, "the sweep never adds a second active job for the same revision")
				if test.moved {
					assert.False(t, jobs[0].RunAt.After(time.Now().UTC()), "the woken run's job is due, got %s", jobs[0].RunAt)
				} else {
					assert.WithinDuration(t, later, jobs[0].RunAt, time.Second, "an acquired job keeps its schedule")
				}
				assert.Equal(t, test.job.Status, jobs[0].Status)
				assert.Equal(t, test.job.LeaseGeneration, jobs[0].LeaseGeneration, "moving a job never touches its lease")
			})
		})
	}
}
