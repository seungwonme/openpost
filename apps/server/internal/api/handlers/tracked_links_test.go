package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v4"
	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/services/trackedlinks"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestTrackedLinkRoutesAuthorizeWorkspaceAndReturnLinks(t *testing.T) {
	db := newHandlerSchemaTestDB(t)
	seedTrackedLinkWorkspace(t, db)
	service := trackedlinks.NewService(db)
	e := echo.New()
	api := humaecho.NewWithGroup(e, e.Group("/api/v1"), huma.DefaultConfig("Test", "1.0.0"))
	authenticator := workspaceTestAuthenticator{
		"owner-token":    {UserID: "user-1"},
		"viewer-token":   {UserID: "viewer-1"},
		"outsider-token": {UserID: "outsider-1"},
	}
	NewTrackedLinkHandler(service, db, authenticator).RegisterRoutes(api)

	create := trackedLinkJSONRequest(t, e, http.MethodPost, "/api/v1/workspaces/ws-1/tracked-links", map[string]any{
		"publication_id": "publication-1", "destination_url": "https://example.com/launch",
	}, "owner-token")
	require.Equal(t, http.StatusCreated, create.Code, create.Body.String())
	var created models.TrackedLink
	require.NoError(t, json.Unmarshal(create.Body.Bytes(), &created))
	require.Equal(t, "ws-1", created.WorkspaceID)
	require.Equal(t, "publication-1", created.PublicationID)
	require.Equal(t, "https://example.com/launch", created.DestinationURL)
	require.NotEmpty(t, created.Slug)

	list := trackedLinkJSONRequest(t, e, http.MethodGet, "/api/v1/workspaces/ws-1/tracked-links", nil, "owner-token")
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	var links []models.TrackedLink
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &links))
	require.Len(t, links, 1)
	require.Equal(t, created.ID, links[0].ID)

	deniedCreate := trackedLinkJSONRequest(t, e, http.MethodPost, "/api/v1/workspaces/ws-1/tracked-links", map[string]any{
		"publication_id": "publication-1", "destination_url": "https://example.com/denied",
	}, "viewer-token")
	require.Equal(t, http.StatusForbidden, deniedCreate.Code, deniedCreate.Body.String())
	viewerList := trackedLinkJSONRequest(t, e, http.MethodGet, "/api/v1/workspaces/ws-1/tracked-links", nil, "viewer-token")
	require.Equal(t, http.StatusOK, viewerList.Code, viewerList.Body.String())
	outsiderList := trackedLinkJSONRequest(t, e, http.MethodGet, "/api/v1/workspaces/ws-1/tracked-links", nil, "outsider-token")
	require.Equal(t, http.StatusForbidden, outsiderList.Code, outsiderList.Body.String())
}

func TestTrackedLinkPublicRedirectsAndFiltersNonHumanRequests(t *testing.T) {
	db := newHandlerSchemaTestDB(t)
	seedTrackedLinkWorkspace(t, db)
	service := trackedlinks.NewService(db)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, link := range []*models.TrackedLink{
		{ID: "tracked-active", WorkspaceID: "ws-1", PublicationID: "publication-1", Slug: "launch", DestinationURL: "https://example.com/launch", CreatedByID: "user-1", IsActive: true, CreatedAt: now, UpdatedAt: now},
		{ID: "tracked-inactive", WorkspaceID: "ws-1", PublicationID: "publication-1", Slug: "old", DestinationURL: "https://example.com/old", CreatedByID: "user-1", IsActive: true, CreatedAt: now, UpdatedAt: now},
	} {
		_, err := db.NewInsert().Model(link).Exec(ctx)
		require.NoError(t, err)
	}
	_, err := db.NewUpdate().Model((*models.TrackedLink)(nil)).Set("is_active = ?", false).Where("id = ?", "tracked-inactive").Exec(ctx)
	require.NoError(t, err)
	e := echo.New()
	NewTrackedLinkHandler(service, db, nil).RegisterPublicRoutes(e)

	success := trackedLinkPublicRequest(t, e, http.MethodGet, "/go/launch", "", "")
	require.Equal(t, http.StatusFound, success.Code)
	require.Equal(t, "https://example.com/launch", success.Header().Get(echo.HeaderLocation))
	require.Empty(t, success.Body.String())

	head := trackedLinkPublicRequest(t, e, http.MethodHead, "/go/launch", "Mozilla/5.0", "")
	require.Equal(t, http.StatusFound, head.Code)
	require.Equal(t, "https://example.com/launch", head.Header().Get(echo.HeaderLocation))

	bot := trackedLinkPublicRequest(t, e, http.MethodGet, "/go/launch", "Twitterbot/1.0", "")
	require.Equal(t, http.StatusFound, bot.Code)
	prefetch := trackedLinkPublicRequest(t, e, http.MethodGet, "/go/launch", "Mozilla/5.0", "prefetch")
	require.Equal(t, http.StatusFound, prefetch.Code)

	missing := trackedLinkPublicRequest(t, e, http.MethodGet, "/go/missing", "Mozilla/5.0", "")
	require.Equal(t, http.StatusNotFound, missing.Code)
	inactive := trackedLinkPublicRequest(t, e, http.MethodGet, "/go/old", "Mozilla/5.0", "")
	require.Equal(t, http.StatusNotFound, inactive.Code)
	invalid := trackedLinkPublicRequest(t, e, http.MethodGet, "/go/%20", "Mozilla/5.0", "")
	require.Equal(t, http.StatusBadRequest, invalid.Code)

	clicks, err := db.NewSelect().Model((*models.TrackedLinkClick)(nil)).Where("tracked_link_id = ?", "tracked-active").Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, clicks)
}

func seedTrackedLinkWorkspace(t *testing.T, db *bun.DB) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	rows := []any{
		&models.User{ID: "user-1", Email: "user@example.com", PasswordHash: "hash", CreatedAt: now},
		&models.User{ID: "viewer-1", Email: "viewer@example.com", PasswordHash: "hash", CreatedAt: now},
		&models.Organization{ID: "org-1", Name: "Launch", CreatedByID: "user-1", CreatedAt: now},
		&models.OrganizationMember{OrganizationID: "org-1", UserID: "user-1", Role: models.OrganizationRoleOwner, CreatedAt: now},
		&models.Workspace{ID: "ws-1", OrganizationID: "org-1", Name: "Launch", CreatedAt: now},
		&models.WorkspaceMember{WorkspaceID: "ws-1", UserID: "user-1", Role: models.WorkspaceRoleEditor, Status: models.WorkspaceMemberStatusActive, CreatedAt: now},
		&models.WorkspaceMember{WorkspaceID: "ws-1", UserID: "viewer-1", Role: models.WorkspaceRoleViewer, Status: models.WorkspaceMemberStatusActive, CreatedAt: now},
		&models.Publication{ID: "publication-1", WorkspaceID: "ws-1", CreatedByID: "user-1", Title: "Launch", ContentProfile: models.ContentProfileShortText, SourceText: "Launch", SourceContent: "Launch", Status: models.PublicationStatusPublished, MetadataJSON: "{}", ReleasePlanJSON: "{}", CreatedAt: now, UpdatedAt: now},
	}
	for _, row := range rows {
		_, err := db.NewInsert().Model(row).Exec(ctx)
		require.NoError(t, err)
	}
}

func trackedLinkJSONRequest(t *testing.T, e *echo.Echo, method, path string, body map[string]any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var data bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&data).Encode(body))
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, &data)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func trackedLinkPublicRequest(t *testing.T, e *echo.Echo, method, path, userAgent, purpose string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	if purpose != "" {
		req.Header.Set("Purpose", purpose)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}
