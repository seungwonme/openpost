package engagement

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/uptrace/bun"
)

// ErrNoDeliveredPrivateReply stops a follow-up that has nothing to continue from.
var ErrNoDeliveredPrivateReply = errors.New("this run has no delivered private reply to follow up on")

// DeliveredPrivateReply says when this run's private reply was accepted and who
// received it. Follow-ups measure the person's response from that moment.
type DeliveredPrivateReply struct {
	ItemID, AccountID, AuthorRemoteID string
	SentAt                            time.Time
	// QuickReplyTitle is the quick reply the private reply carried, if any.
	QuickReplyTitle string
}

// RunPrivateReply reads stored receipts only. It never calls a provider.
func (s *Service) RunPrivateReply(ctx context.Context, authority workspaceaccess.StoredAuthority, runID, itemID string) (DeliveredPrivateReply, error) {
	decision, err := workspaceaccess.NewAuthorizer(s.db).AuthorizeStored(ctx, authority, workspaceaccess.LevelAdminister)
	if err != nil {
		return DeliveredPrivateReply{}, err
	}
	if !decision.Allowed {
		return DeliveredPrivateReply{}, ErrAccessDenied
	}
	var reply AutomationReply
	err = s.db.NewSelect().Model(&reply).Where("workspace_id = ? AND run_id = ? AND item_id = ? AND channel = 'private' AND state = 'sent'", authority.WorkspaceID, runID, itemID).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return DeliveredPrivateReply{}, ErrNoDeliveredPrivateReply
	}
	if err != nil {
		return DeliveredPrivateReply{}, err
	}
	var item models.EngagementItem
	if err := s.db.NewSelect().Model(&item).Where("id = ? AND workspace_id = ?", itemID, authority.WorkspaceID).Scan(ctx); err != nil {
		return DeliveredPrivateReply{}, ErrNotFound
	}
	title, err := runStepQuickReply(ctx, s.db, runID, reply.StepID)
	if err != nil {
		return DeliveredPrivateReply{}, err
	}
	return DeliveredPrivateReply{ItemID: item.ID, AccountID: item.SocialAccountID, AuthorRemoteID: item.AuthorRemoteID, SentAt: reply.UpdatedAt, QuickReplyTitle: title}, nil
}

type snapshotStep struct {
	ID     string `json:"id"`
	Inputs map[string]struct {
		Literal any `json:"literal"`
	} `json:"inputs"`
	Then []snapshotStep `json:"then"`
	Else []snapshotStep `json:"else"`
}

// runStepQuickReply reads the quick reply title from the run's own definition
// snapshot, which is what the private reply step sent.
func runStepQuickReply(ctx context.Context, db bun.IDB, runID, stepID string) (string, error) {
	var raw string
	if err := db.NewSelect().Table("workflow_runs").Column("definition_json").Where("id = ?", runID).Scan(ctx, &raw); err != nil {
		return "", err
	}
	var definition struct {
		Steps []snapshotStep `json:"steps"`
	}
	if err := json.Unmarshal([]byte(raw), &definition); err != nil {
		return "", err
	}
	var find func([]snapshotStep) string
	find = func(steps []snapshotStep) string {
		for _, step := range steps {
			if step.ID == stepID {
				title, _ := step.Inputs["quick_reply_title"].Literal.(string)
				return strings.TrimSpace(title)
			}
			if title := find(step.Then); title != "" {
				return title
			}
			if title := find(step.Else); title != "" {
				return title
			}
		}
		return ""
	}
	return find(definition.Steps), nil
}
