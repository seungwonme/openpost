package workflows

import (
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/uptrace/bun"
)

// maxCommentBackfillHours bounds include_recent_comments_hours. A campaign
// never answers comments written more than this long before the current poll.
const maxCommentBackfillHours = 7 * 24

func validateCommentBackfill(source Source) error {
	hours := source.IncludeRecentCommentsHours
	if hours == 0 {
		return nil
	}
	if source.Kind != "comment_received" {
		return invalid("recent comments apply only to comment triggers")
	}
	if hours < 0 || hours > maxCommentBackfillHours {
		return invalid("include recent comments from 0 to 168 hours")
	}
	return nil
}

// commentAdmissionSince is the lower admission bound for a comment source that
// started at started. The window is anchored to the start that publishing fixed,
// so it never slides with later polls, and it is clamped so a comment older than
// seven days before now never starts a run. Without the option the bound stays
// started.
func commentAdmissionSince(started time.Time, source Source, now time.Time) time.Time {
	if source.IncludeRecentCommentsHours <= 0 {
		return started
	}
	since := started.Add(-time.Duration(source.IncludeRecentCommentsHours) * time.Hour)
	if floor := now.Add(-maxCommentBackfillHours * time.Hour); since.Before(floor) {
		since = floor
	}
	if since.After(started) {
		return started
	}
	return since
}

// commentBackfillFilter keeps comments from before the source started out of a
// campaign when a person already answered them, Inbox took them over, or any
// campaign already replied. Newer comments keep the step-level checks.
func commentBackfillFilter(query *bun.SelectQuery, source Source, started time.Time) *bun.SelectQuery {
	if source.IncludeRecentCommentsHours <= 0 {
		return query
	}
	return query.WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("engagement_item.remote_created_at >= ? AND engagement_item.created_at >= ?", started, started).
			WhereOr("engagement_item.replied_at IS NULL AND engagement_item.automation_stopped_at IS NULL AND NOT EXISTS (SELECT 1 FROM engagement_automation_replies a WHERE a.item_id = engagement_item.id)")
	})
}

// commentAdmitted applies the same bounds to another campaign's candidate
// check, so overlap resolution sees backfilled comments the way polling does.
func commentAdmitted(started time.Time, source Source, item models.EngagementItem, now time.Time) bool {
	since := commentAdmissionSince(started, source, now)
	if item.RemoteCreatedAt.Before(since) || item.CreatedAt.Before(since) {
		return false
	}
	backfilled := item.RemoteCreatedAt.Before(started) || item.CreatedAt.Before(started)
	return !backfilled || (item.RepliedAt.IsZero() && item.AutomationStoppedAt.IsZero())
}
