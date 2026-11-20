// Package appbranding owns application identity independently of theme documents
// and the Workspace's content-creation brand kit.
package appbranding

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/openpost/backend/internal/jobregistry"
	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/services/identity"
	"github.com/openpost/backend/internal/services/mediastore"
	"github.com/openpost/backend/internal/services/organizationguard"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/uptrace/bun"
)

var (
	ErrDenied   = errors.New("app branding access denied")
	ErrInvalid  = errors.New("invalid app branding")
	ErrConflict = errors.New("app branding changed; reload before saving")
	ErrLocked   = errors.New("workspace branding is managed by the organization")
)

type Actor = workspaceaccess.ActorFacts

//nolint:revive // OpenAPI schemas share a global registry; the prefix prevents collisions.
type AppBrandingImage struct {
	ID  string `json:"id"`
	URL string `json:"url,omitempty"`
}

//nolint:revive // OpenAPI schemas share a global registry; the prefix prevents collisions.
type AppBrandingProfile struct {
	DisplayName  string `json:"display_name" maxLength:"60"`
	Logo         *Image `json:"logo,omitempty"`
	DarkLogo     *Image `json:"dark_logo,omitempty"`
	Wordmark     *Image `json:"wordmark,omitempty"`
	DarkWordmark *Image `json:"dark_wordmark,omitempty"`
}

//nolint:revive // OpenAPI schemas share a global registry; the prefix prevents collisions.
type AppBrandingImageInput struct {
	ID            string `json:"id,omitempty" doc:"Retain an image from this profile or copy the current organization image"`
	ContentBase64 string `json:"content_base64,omitempty" maxLength:"1400000" doc:"PNG, JPEG or WebP, at most 1 MiB; normalized to PNG"`
}

//nolint:revive // OpenAPI schemas share a global registry; the prefix prevents collisions.
type AppBrandingProfileInput struct {
	DisplayName  string      `json:"display_name" minLength:"1" maxLength:"60"`
	Logo         *ImageInput `json:"logo,omitempty"`
	DarkLogo     *ImageInput `json:"dark_logo,omitempty"`
	Wordmark     *ImageInput `json:"wordmark,omitempty"`
	DarkWordmark *ImageInput `json:"dark_wordmark,omitempty"`
}

//nolint:revive // OpenAPI schemas share a global registry; the prefix prevents collisions.
type AppBrandingSettings struct {
	WorkspaceID            string   `json:"workspace_id"`
	OrganizationID         string   `json:"organization_id"`
	OrganizationDefault    Profile  `json:"organization_default"`
	OrganizationRevision   int      `json:"organization_revision"`
	WorkspaceOverride      *Profile `json:"workspace_override,omitempty"`
	WorkspaceRevision      int      `json:"workspace_revision"`
	Effective              Profile  `json:"effective"`
	AllowWorkspaceOverride bool     `json:"allow_workspace_override"`
	CanManageOrganization  bool     `json:"can_manage_organization"`
	CanManageWorkspace     bool     `json:"can_manage_workspace"`
}

//nolint:revive // OpenAPI schemas share a global registry; the prefix prevents collisions.
type AppBrandingUpdate struct {
	ExpectedRevision       int           `json:"expected_revision" minimum:"0"`
	Profile                *ProfileInput `json:"profile,omitempty" doc:"Null restores OpenPost for an organization or inheritance for a workspace"`
	AllowWorkspaceOverride bool          `json:"allow_workspace_override,omitempty"`
}

type profileRow struct {
	bun.BaseModel          `bun:"table:app_branding_profiles"`
	ScopeID                string    `bun:"scope_id,pk"`
	OrganizationID         string    `bun:"organization_id"`
	WorkspaceID            *string   `bun:"workspace_id"`
	Revision               int       `bun:"revision"`
	ProfileJSON            string    `bun:"profile_json"`
	AllowWorkspaceOverride bool      `bun:"allow_workspace_override"`
	UpdatedBy              string    `bun:"updated_by"`
	UpdatedAt              time.Time `bun:"updated_at"`
}

type Service struct {
	db      *bun.DB
	storage mediastore.BlobStorage
}

func New(db *bun.DB, storage mediastore.BlobStorage) *Service {
	return &Service{db: db, storage: storage}
}
func Default() Profile { return Profile{DisplayName: "Aiden OpenPost"} }
func scope(organizationID, workspaceID string) string {
	if workspaceID != "" {
		return "workspace:" + workspaceID
	}
	return "organization:" + organizationID
}
func load(ctx context.Context, db bun.IDB, organizationID, workspaceID string) (profileRow, Profile, error) {
	row := profileRow{ScopeID: scope(organizationID, workspaceID), OrganizationID: organizationID}
	if workspaceID != "" {
		row.WorkspaceID = &workspaceID
	}
	err := db.NewSelect().Model(&row).Where("scope_id = ? AND organization_id = ?", row.ScopeID, organizationID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return row, Default(), nil
	}
	if err != nil {
		return row, Profile{}, err
	}
	var profile Profile
	if row.ProfileJSON == "null" {
		return row, Default(), nil
	}
	if err = json.Unmarshal([]byte(row.ProfileJSON), &profile); err != nil {
		return row, Profile{}, err
	}
	return row, profile, nil
}
func authorize(ctx context.Context, db bun.IDB, actor Actor, workspaceID string, level workspaceaccess.Level) (string, error) {
	decision, err := workspaceaccess.NewAuthorizer(db).Authorize(ctx, workspaceID, actor, level)
	if err != nil {
		return "", err
	}
	if !decision.Allowed {
		return "", ErrDenied
	}
	return decision.OrganizationID, nil
}
func organizationAdmin(ctx context.Context, db bun.IDB, actor Actor, organizationID string) (bool, error) {
	if actor.UserID == "" || actor.CredentialWorkspaceID != "" || actor.ClientID != "" {
		return false, nil
	}
	var member models.OrganizationMember
	err := db.NewSelect().Model(&member).Where("organization_id = ? AND user_id = ?", organizationID, actor.UserID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if member.Role != models.OrganizationRoleOwner && member.Role != models.OrganizationRoleAdmin {
		return false, nil
	}
	decision, err := identity.EvaluateOrganizationAccess(ctx, db, organizationID, actor.UserID, actor.SessionID, actor.TokenID)
	return decision.Allowed, err
}

func (s *Service) Settings(ctx context.Context, actor Actor, workspaceID string) (Settings, error) {
	organizationID, err := authorize(ctx, s.db, actor, workspaceID, workspaceaccess.LevelRead)
	if err != nil {
		return Settings{}, err
	}
	orgRow, org, err := load(ctx, s.db, organizationID, "")
	if err != nil {
		return Settings{}, err
	}
	workspaceRow, workspace, err := load(ctx, s.db, organizationID, workspaceID)
	if err != nil {
		return Settings{}, err
	}
	manageOrg, err := organizationAdmin(ctx, s.db, actor, organizationID)
	if err != nil {
		return Settings{}, err
	}
	_, manageErr := authorize(ctx, s.db, actor, workspaceID, workspaceaccess.LevelAdminister)
	if manageErr != nil && !errors.Is(manageErr, ErrDenied) {
		return Settings{}, manageErr
	}
	result := Settings{WorkspaceID: workspaceID, OrganizationID: organizationID, OrganizationDefault: withURLs(org, workspaceID, true), OrganizationRevision: orgRow.Revision, WorkspaceRevision: workspaceRow.Revision, Effective: withURLs(org, workspaceID, false), AllowWorkspaceOverride: orgRow.AllowWorkspaceOverride, CanManageOrganization: manageOrg, CanManageWorkspace: manageErr == nil}
	if orgRow.AllowWorkspaceOverride && workspaceRow.Revision > 0 && workspaceRow.ProfileJSON != "null" {
		p := withURLs(workspace, workspaceID, false)
		result.WorkspaceOverride = &p
		result.Effective = p
	}
	// Organization-default assets are previewable only by administrators. Ordinary
	// members receive only the effective profile, with workspace-authorized URLs.
	if !manageOrg && manageErr != nil {
		result.OrganizationDefault = Profile{DisplayName: org.DisplayName}
	}
	return result, nil
}

func (s *Service) Save(ctx context.Context, actor Actor, workspaceID string, organization bool, input Update) (Settings, error) {
	organizationID, err := authorize(ctx, s.db, actor, workspaceID, workspaceaccess.LevelRead)
	if err != nil {
		return Settings{}, err
	}
	created := []string{}
	err = organizationguard.WithOrganization(ctx, s.db, organizationID, func(txCtx context.Context, db bun.IDB) error {
		if _, err := authorize(txCtx, db, actor, workspaceID, workspaceaccess.LevelRead); err != nil {
			return err
		}
		orgRow, orgProfile, err := load(txCtx, db, organizationID, "")
		if err != nil {
			return err
		}
		targetWorkspaceID, err := authorizeWrite(txCtx, db, actor, workspaceID, organization, orgRow)
		if err != nil {
			return err
		}

		row, old, err := load(txCtx, db, organizationID, targetWorkspaceID)
		if err != nil {
			return err
		}
		if row.Revision != input.ExpectedRevision {
			return ErrConflict
		}
		next := Default()
		if input.Profile != nil {
			next, err = s.prepare(txCtx, organizationID, input.Profile, old, orgProfile, organization, &created)
			if err != nil {
				return err
			}
		}
		if err := persist(txCtx, db, actor.UserID, row, next, input); err != nil {
			return err
		}
		removed := removedKeys(organizationID, old, next)
		retired, err := retireOverrides(txCtx, db, actor.UserID, organizationID, organization, input.AllowWorkspaceOverride)
		if err != nil {
			return err
		}
		removed = append(removed, retired...)

		if len(removed) > 0 {
			_, err = jobregistry.EnqueueStorageDeletes(txCtx, db, removed)
		}
		return err
	})
	if err != nil {
		s.cleanupUncommitted(ctx, created)
		return Settings{}, err
	}
	return s.Settings(ctx, actor, workspaceID)
}

func authorizeWrite(ctx context.Context, db bun.IDB, actor Actor, workspaceID string, organization bool, settings profileRow) (string, error) {
	if organization {
		admin, err := organizationAdmin(ctx, db, actor, settings.OrganizationID)
		if err != nil {
			return "", err
		}
		if !admin {
			return "", ErrDenied
		}
		return "", nil
	}
	if _, err := authorize(ctx, db, actor, workspaceID, workspaceaccess.LevelAdminister); err != nil {
		return "", err
	}
	if !settings.AllowWorkspaceOverride {
		return "", ErrLocked
	}
	return workspaceID, nil
}

func persist(ctx context.Context, db bun.IDB, actorID string, row profileRow, next Profile, input Update) error {
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if row.WorkspaceID != nil && input.Profile == nil {
		raw = []byte("null")
	}
	row.ProfileJSON = string(raw)
	row.Revision++
	row.UpdatedBy = actorID
	row.UpdatedAt = time.Now().UTC()
	row.AllowWorkspaceOverride = row.WorkspaceID == nil && input.AllowWorkspaceOverride
	_, err = db.NewInsert().Model(&row).On("CONFLICT (scope_id) DO UPDATE").Set("profile_json = EXCLUDED.profile_json").Set("revision = EXCLUDED.revision").Set("allow_workspace_override = EXCLUDED.allow_workspace_override").Set("updated_by = EXCLUDED.updated_by").Set("updated_at = EXCLUDED.updated_at").Exec(ctx)
	return err
}

func retireOverrides(ctx context.Context, db bun.IDB, actorID, organizationID string, organization, allow bool) ([]string, error) {
	if !organization || allow {
		return nil, nil
	}
	keys, err := storedKeys(ctx, db, organizationID, "", true)
	if err != nil {
		return nil, err
	}
	_, err = db.NewUpdate().Model((*profileRow)(nil)).Set("profile_json = ?", "null").Set("revision = revision + 1").Set("updated_by = ?", actorID).Set("updated_at = ?", time.Now().UTC()).Where("organization_id = ? AND workspace_id IS NOT NULL", organizationID).Exec(ctx)
	return keys, err
}

func normalizedName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 60 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("%w: use a display name of 1 to 60 characters without control characters", ErrInvalid)
	}
	return name, nil
}

// Short internal names keep the service concise; named API schemas are globally unique.
type Profile = AppBrandingProfile
type Image = AppBrandingImage
type ProfileInput = AppBrandingProfileInput
type ImageInput = AppBrandingImageInput
type Settings = AppBrandingSettings
type Update = AppBrandingUpdate
