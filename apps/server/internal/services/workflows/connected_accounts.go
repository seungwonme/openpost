package workflows

import (
	"context"
	"strings"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/uptrace/bun"
)

// ConnectedAccountSkipPrefix opens every reason recorded when a comment came
// from an account connected to the same workspace. Run history, the Inbox
// summary and the daily limit all recognize a skipped comment by it.
const ConnectedAccountSkipPrefix = "The commenter is an account connected to this workspace, so this comment did not start the campaign."

const (
	connectedMatchID       = "id"
	connectedMatchPossible = "possible"
)

// ConnectedAccountMatch names the connected account a commenter resolved to and
// which identity matched, so a false positive can be diagnosed from the reason.
// The remote ID is authoritative. A username match is only `possible`: Instagram
// reports different IDs on different surfaces and login modes, and an account
// can be renamed.
type ConnectedAccountMatch struct {
	SocialAccountID string
	By              string
	Username        string
}

// Reason is the visible explanation stored on the cancelled run and returned
// by the write fence.
func (m ConnectedAccountMatch) Reason() string {
	if m.By == connectedMatchID {
		return ConnectedAccountSkipPrefix + " Matched by account ID."
	}
	return ConnectedAccountSkipPrefix + " Possible match by username @" + m.Username + "."
}

type connectedAccount struct {
	id, platform, remoteID, username string
}

// ConnectedAccounts is the set of accounts connected in one workspace, active or
// not, loaded once so a page of comments is matched without a query per comment.
type ConnectedAccounts struct{ accounts []connectedAccount }

// LoadConnectedAccounts reads every social account in the workspace.
func LoadConnectedAccounts(ctx context.Context, db bun.IDB, workspaceID string) (ConnectedAccounts, error) {
	var rows []models.SocialAccount
	if err := db.NewSelect().Model(&rows).Column("id", "platform", "account_id", "account_username").Where("workspace_id = ?", workspaceID).Scan(ctx); err != nil {
		return ConnectedAccounts{}, err
	}
	set := ConnectedAccounts{accounts: make([]connectedAccount, 0, len(rows))}
	for _, row := range rows {
		set.accounts = append(set.accounts, connectedAccount{id: row.ID, platform: row.Platform, remoteID: strings.TrimSpace(row.AccountID), username: normalizeHandle(row.AccountUsername)})
	}
	return set, nil
}

// Match resolves a commenter on the given platform. The remote ID wins over any
// username match, whatever the order of the accounts.
func (c ConnectedAccounts) Match(platform, remoteID string, handles ...string) (ConnectedAccountMatch, bool) {
	remoteID = strings.TrimSpace(remoteID)
	if remoteID != "" {
		for _, account := range c.accounts {
			if account.platform == platform && account.remoteID == remoteID {
				return ConnectedAccountMatch{SocialAccountID: account.id, By: connectedMatchID, Username: account.username}, true
			}
		}
	}
	for _, handle := range handles {
		handle = normalizeHandle(handle)
		if handle == "" {
			continue
		}
		for _, account := range c.accounts {
			if account.platform == platform && account.username == handle {
				return ConnectedAccountMatch{SocialAccountID: account.id, By: connectedMatchPossible, Username: account.username}, true
			}
		}
	}
	return ConnectedAccountMatch{}, false
}

// FindConnectedAccount is the single-comment form the engagement and messaging
// write fences use, so they apply the same identity rule as admission.
func FindConnectedAccount(ctx context.Context, db bun.IDB, item models.EngagementItem) (ConnectedAccountMatch, bool, error) {
	accounts, err := LoadConnectedAccounts(ctx, db, item.WorkspaceID)
	if err != nil {
		return ConnectedAccountMatch{}, false, err
	}
	match, found := accounts.Match(item.Platform, item.AuthorRemoteID, item.AuthorHandle, item.AuthorName)
	return match, found, nil
}

func normalizeHandle(value string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), "@"))
}

// recordSkippedRun creates the run for an admitted event and ends it at once with
// the reason, the way a sender over the daily limit is recorded. The run has no
// steps, so nothing is sent and the run history says why.
func (s *Service) recordSkippedRun(ctx context.Context, tx bun.Tx, record workflowRecord, def Definition, authority workspaceaccess.StoredAuthority, item SourceItem, reason string, now time.Time) error {
	source, err := sourceItemValues(item)
	if err != nil {
		return err
	}
	run, err := s.createRun(ctx, tx, record, def, authority, ModeLive, source)
	if err != nil {
		return err
	}
	_, err = tx.NewUpdate().Model(&run).Set("state = ?, error = ?, revision = revision + 1, wake_at = NULL, updated_at = ?", StateCancelled, reason, now).WherePK().Exec(ctx)
	return err
}

// warnConnectedCommenter lets a preview or test run use a sibling account on
// purpose. It shows the match as a warning on the step; a live run is stopped
// at admission and again before the write instead.
func (s *Service) warnConnectedCommenter(ctx context.Context, record runRecord, step Step, output, inputs map[string]any) {
	id, _ := inputs["engagement_id"].(string)
	if record.Mode == ModeLive || !isCommentAction(step.Kind) || id == "" {
		return
	}
	var item models.EngagementItem
	if err := s.db.NewSelect().Model(&item).Where("id = ? AND workspace_id = ?", id, record.WorkspaceID).Scan(ctx); err != nil {
		return
	}
	if match, found, err := FindConnectedAccount(ctx, s.db, item); err == nil && found {
		output["warning"] = "Warning: " + match.Reason() + " A live run would skip it."
	}
}
