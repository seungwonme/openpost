package engagement

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/openpost/backend/internal/inboxactivity"
	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/organizationguard"
	"github.com/uptrace/bun"
)

const (
	// Comment reads per job. Media left over keep their previous count and are
	// read by the next job, one minute later.
	nativeCommentReadsPerJob = 10
	// Below the five-minute engagement sweep, so every sweep collects again.
	nativeCommentCadence = 4 * time.Minute
	nativeCommentCatchUp = time.Minute
	// Matches the owned-post collection window.
	nativeCommentMaxAge = 90 * 24 * time.Hour
)

// nativeCommentCheckpoint is the feed cursor: the last comment count read for
// each recent media item. A generation reset clears it, which reads every
// media item with comments once more.
type nativeCommentCheckpoint struct {
	Version int            `json:"v"`
	Counts  map[string]int `json:"counts"`
}

type nativeCommentPlan struct {
	read     []platform.InstagramMediaSummary
	counts   map[string]int
	deferred bool
}

type nativeMediaComments struct {
	media    platform.InstagramMediaSummary
	comments []platform.Comment
}

func decodeNativeCommentCheckpoint(cursor string) map[string]int {
	var checkpoint nativeCommentCheckpoint
	if cursor == "" || json.Unmarshal([]byte(cursor), &checkpoint) != nil || checkpoint.Version != 1 || checkpoint.Counts == nil {
		return map[string]int{}
	}
	return checkpoint.Counts
}

func encodeNativeCommentCheckpoint(counts map[string]int) (string, error) {
	raw, err := json.Marshal(nativeCommentCheckpoint{Version: 1, Counts: counts})
	return string(raw), err
}

// planNativeCommentReads reads a media item only when its comment count differs
// from the last count read, or when it is new and has comments. Media published
// through OpenPost belong to owned-post collection and are skipped.
func planNativeCommentReads(previous map[string]int, media []platform.InstagramMediaSummary, owned map[string]bool, now time.Time) nativeCommentPlan {
	plan := nativeCommentPlan{counts: map[string]int{}}
	for _, item := range media {
		if owned[item.ID] || (!item.PublishedAt.IsZero() && item.PublishedAt.Before(now.Add(-nativeCommentMaxAge))) {
			continue
		}
		last, seen := previous[item.ID]
		if (seen && last == item.CommentsCount) || (!seen && item.CommentsCount == 0) {
			plan.counts[item.ID] = item.CommentsCount
			continue
		}
		if seen {
			plan.counts[item.ID] = last
		}
		if len(plan.read) == nativeCommentReadsPerJob {
			plan.deferred = true
			continue
		}
		plan.read = append(plan.read, item)
	}
	return plan
}

func (s *Service) ownedInstagramMedia(ctx context.Context, accountID string, media []platform.InstagramMediaSummary) (map[string]bool, error) {
	owned := map[string]bool{}
	if len(media) == 0 {
		return owned, nil
	}
	ids := make([]string, 0, len(media))
	for _, item := range media {
		ids = append(ids, item.ID)
	}
	var external []string
	if err := s.db.NewSelect().Model((*models.Rendition)(nil)).Column("external_id").
		Where("social_account_id=? AND status=? AND external_id IN (?)", accountID, models.RenditionStatusPublished, bun.List(ids)).Scan(ctx, &external); err != nil {
		return nil, err
	}
	for _, id := range external {
		owned[id] = true
	}
	return owned, nil
}

// collectInstagramNativeComments lists the account's recent media in one
// request and reads comments only where the count changed. Comments are stored
// exactly like webhook-delivered comments on media published outside OpenPost.
func (s *Service) collectInstagramNativeComments(ctx context.Context, input accountActivityJob, token string) error {
	state, authority, adapter, err := s.activityCheckpoint(ctx, s.db, input)
	if errors.Is(err, errActivityChanged) || errors.Is(err, errActivityInactive) {
		return nil
	}
	if err != nil {
		return err
	}
	reader, ok := adapter.(platform.InstagramNativeCommentReader)
	if !ok {
		return s.collectAccountActivity(ctx, input, token)
	}
	media, err := reader.ListRecentMedia(ctx, token, authority.Account.AccountID)
	if err != nil {
		return s.recordActivityFailure(ctx, state, err)
	}
	owned, err := s.ownedInstagramMedia(ctx, authority.Account.ID, media)
	if err != nil {
		return err
	}
	plan := planNativeCommentReads(decodeNativeCommentCheckpoint(state.Cursor), media, owned, s.now())
	reads := make([]nativeMediaComments, 0, len(plan.read))
	var failure error
	for _, item := range plan.read {
		comments, err := reader.ListComments(ctx, token, authority.Account.AccountID, item.ID)
		if err != nil {
			failure = err
			break
		}
		reads = append(reads, nativeMediaComments{media: item, comments: comments})
		plan.counts[item.ID] = item.CommentsCount
	}
	cursor, err := encodeNativeCommentCheckpoint(plan.counts)
	if err != nil {
		return err
	}
	next := nativeCommentCadence
	if plan.deferred {
		next = nativeCommentCatchUp
	}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := organizationguard.LockWorkspace(ctx, tx, authority.Account.WorkspaceID); err != nil {
			return err
		}
		current, latest, _, err := s.activityCheckpoint(ctx, tx, input)
		if err != nil {
			return err
		}
		if current.Cursor != state.Cursor || latest.Account.GrantedScopes != authority.Account.GrantedScopes {
			return errActivityChanged
		}
		for _, read := range reads {
			for _, comment := range read.comments {
				comment.ConversationID = read.media.ID
				// Comments written before this feed started are history, not news.
				created := platform.ProviderTime(comment.CreatedAt)
				notify := created.IsZero() || !created.Before(current.CreatedAt)
				if err := s.persistExternalInstagramComment(ctx, tx, latest.Account, comment, read.media.Permalink, read.media.ID, notify); err != nil {
					return err
				}
			}
		}
		now := s.now()
		_, err = tx.NewUpdate().Model((*models.EngagementAccountSyncState)(nil)).
			Set("cursor=?", cursor).Set("status=?", "ok").Set("error_code=?", "").Set("error_message=?", "").
			Set("last_attempted_at=?", now).Set("last_success_at=?", now).Set("next_sync_at=?", now.Add(next)).Set("updated_at=?", now).
			Where("social_account_id=? AND feed=? AND generation=?", input.AccountID, input.Feed, input.Generation).Exec(ctx)
		return err
	})
	if errors.Is(err, errActivityChanged) || errors.Is(err, errActivityInactive) {
		return nil
	}
	if err != nil {
		return err
	}
	if failure != nil {
		return s.recordActivityFailure(ctx, state, failure)
	}
	return nil
}

// persistExternalInstagramComment stores a comment on media published outside
// OpenPost. Webhook delivery and polling share it, so a comment seen by both
// keeps one Inbox item and one notification.
func (s *Service) persistExternalInstagramComment(ctx context.Context, tx bun.Tx, account models.SocialAccount, comment platform.Comment, permalink, postID string, notify bool) error {
	item, isNew, err := persistEngagementComment(ctx, tx, models.Rendition{}, account, comment, s.now())
	if err != nil {
		return err
	}
	if item.ID == "" {
		return nil
	}
	subject := inboxactivity.EngagementSubject{Kind: "comment", RemoteID: postID, URL: safeExternalURL(permalink)}
	raw, err := json.Marshal(subject)
	if err != nil {
		return err
	}
	item.SubjectJSON = string(raw)
	if err := saveEngagementItem(ctx, tx, item); err != nil {
		return err
	}
	if isNew && notify && s.notifications != nil {
		return s.notifyAccountActivity(ctx, tx, account, item)
	}
	return nil
}
