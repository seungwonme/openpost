package engagement

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/services/workspaceaccess"
)

// ErrNoDeliveredPrivateReply stops a follow-up that has nothing to continue from.
var ErrNoDeliveredPrivateReply = errors.New("this run has no delivered private reply to follow up on")

// DeliveredPrivateReply says when this run's private reply was accepted and who
// received it. Follow-ups measure the person's response from that moment.
type DeliveredPrivateReply struct {
	ItemID, AccountID, AuthorRemoteID string
	SentAt                            time.Time
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
	return DeliveredPrivateReply{ItemID: item.ID, AccountID: item.SocialAccountID, AuthorRemoteID: item.AuthorRemoteID, SentAt: reply.UpdatedAt}, nil
}
