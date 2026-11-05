package messaging

import (
	"context"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/services/organizationguard"
	"github.com/uptrace/bun"
)

// The takeover and the write fence serialize on the workspace lock. Once a stop
// commits, a send that has not entered the fence cannot reach the provider.
func (s *Service) automationSendGuard(message models.DirectMessage) func(context.Context, bun.Tx) error {
	return func(ctx context.Context, tx bun.Tx) error {
		_, owned, err := s.automationOwnerOfMessage(ctx, tx, message.ID)
		if err != nil || !owned {
			return err
		}
		if err := organizationguard.LockWorkspace(ctx, tx, message.WorkspaceID); err != nil {
			return err
		}
		return s.checkAutomatedSend(ctx, tx, message)
	}
}
