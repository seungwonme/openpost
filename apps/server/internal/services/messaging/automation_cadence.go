package messaging

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/openpost/backend/internal/models"
)

// messageWorkflowCadence is the longest background interval for an account an
// enabled message_received workflow watches. A quiet account otherwise backs
// off to six hours, and the workflow would answer that late.
const messageWorkflowCadence = 15 * time.Minute

// automationCadence keeps the quiet-account backoff from slowing message
// workflows. It never lengthens the proposed cadence.
func (s *Service) automationCadence(ctx context.Context, account models.SocialAccount, backoff time.Duration) time.Duration {
	if backoff <= messageWorkflowCadence {
		return backoff
	}
	watched, err := s.messageWorkflowWatches(ctx, account)
	if err != nil || !watched {
		return backoff
	}
	return messageWorkflowCadence
}

func (s *Service) messageWorkflowWatches(ctx context.Context, account models.SocialAccount) (bool, error) {
	var published []string
	if err := s.db.NewSelect().Table("workflows").Column("published_json").
		Where("workspace_id = ? AND enabled = ?", account.WorkspaceID, true).Scan(ctx, &published); err != nil {
		return false, err
	}
	for _, raw := range published {
		var definition struct {
			Source struct {
				Kind       string   `json:"kind"`
				AccountIDs []string `json:"account_ids"`
			} `json:"source"`
		}
		if json.Unmarshal([]byte(raw), &definition) != nil {
			continue
		}
		if definition.Source.Kind == "message_received" && slices.Contains(definition.Source.AccountIDs, account.ID) {
			return true, nil
		}
	}
	return false, nil
}
