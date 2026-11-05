// Package automationoptout owns the opt-out record that comment and direct message
// automation share. A person opts out per connected account, across both channels.
package automationoptout

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// State is what Inbox shows about one person's opt-out.
type State struct {
	OptedOutAt    *time.Time `json:"opted_out_at,omitempty"`
	RemovedAt     *time.Time `json:"opt_out_removed_at,omitempty"`
	RemovedByName string     `json:"opt_out_removed_by,omitempty"`
}

// Record stores an opt-out requested at the given time. A provider can deliver the
// same message or comment again, so one no newer than the latest removal stays
// removed. An unknown time cannot be shown to be newer.
func Record(ctx context.Context, db bun.IDB, accountID, author string, requestedAt, now time.Time) (bool, error) {
	removed := db.NewSelect().Table("automation_optout_removals").Where("social_account_id = ? AND author_remote_id = ?", accountID, author)
	if !requestedAt.IsZero() {
		removed = removed.Where("removed_at >= ?", requestedAt)
	}
	blocked, err := removed.Exists(ctx)
	if err != nil || blocked {
		return false, err
	}
	result, err := db.NewRaw("INSERT INTO engagement_automation_optouts(social_account_id,author_remote_id,created_at) VALUES(?,?,?) ON CONFLICT DO NOTHING", accountID, author, now).Exec(ctx)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

// Read reports the current opt-out and the latest removal.
func Read(ctx context.Context, db bun.IDB, accountID, author string) (State, error) {
	var state State
	var optedOutAt time.Time
	err := db.NewSelect().Table("engagement_automation_optouts").Column("created_at").Where("social_account_id = ? AND author_remote_id = ?", accountID, author).Scan(ctx, &optedOutAt)
	switch {
	case err == nil:
		state.OptedOutAt = &optedOutAt
	case !errors.Is(err, sql.ErrNoRows):
		return state, err
	}
	var removal struct {
		RemovedAt time.Time `bun:"removed_at"`
		Name      string    `bun:"name"`
	}
	err = db.NewRaw(`SELECT r.removed_at, COALESCE(NULLIF(u.display_name, ''), u.email, '') AS name FROM automation_optout_removals r LEFT JOIN users u ON u.id = r.removed_by WHERE r.social_account_id = ? AND r.author_remote_id = ? ORDER BY r.removed_at DESC, r.id DESC LIMIT 1`, accountID, author).Scan(ctx, &removal)
	switch {
	case err == nil:
		state.RemovedAt, state.RemovedByName = &removal.RemovedAt, removal.Name
	case !errors.Is(err, sql.ErrNoRows):
		return state, err
	}
	return state, nil
}

// Remove deletes the opt-out and records who removed it and when. It reports
// whether there was one to remove.
func Remove(ctx context.Context, tx bun.Tx, workspaceID, accountID, author, removedBy string, now time.Time) (bool, error) {
	var optedOutAt time.Time
	err := tx.NewSelect().Table("engagement_automation_optouts").Column("created_at").Where("social_account_id = ? AND author_remote_id = ?", accountID, author).Scan(ctx, &optedOutAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.NewRaw("DELETE FROM engagement_automation_optouts WHERE social_account_id = ? AND author_remote_id = ?", accountID, author).Exec(ctx); err != nil {
		return false, err
	}
	_, err = tx.NewRaw("INSERT INTO automation_optout_removals(id,workspace_id,social_account_id,author_remote_id,removed_by,opted_out_at,removed_at) VALUES(?,?,?,?,?,?,?)", uuid.NewString(), workspaceID, accountID, author, removedBy, optedOutAt, now).Exec(ctx)
	return err == nil, err
}
