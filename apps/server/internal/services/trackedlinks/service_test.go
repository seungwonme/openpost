package trackedlinks

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/openpost/backend/internal/database"
	"github.com/openpost/backend/internal/models"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func testDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := database.InitDB("file:" + t.TempDir() + "/tracked.db?mode=rwc")
	require.NoError(t, err)
	require.NoError(t, database.CreateSchema(db))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func seed(t *testing.T, db *bun.DB) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	_, err := db.NewInsert().Model(&models.User{ID: "u1", Email: "u1@example.test", PasswordHash: "hash", CreatedAt: now}).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewInsert().Model(&models.Workspace{ID: "w1", Name: "Workspace", CreatedAt: now}).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewInsert().Model(&models.Publication{ID: "p1", WorkspaceID: "w1", CreatedByID: "u1", Title: "Post", SourceContent: "", Status: models.PublicationStatusDraft, CreatedAt: now, UpdatedAt: now}).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewInsert().Model(&models.SocialAccount{ID: "a1", WorkspaceID: "w1", Slug: "a1", Platform: "youtube", AccountID: "remote", AccessTokenEnc: []byte("x"), IsActive: true, CreatedAt: now}).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewInsert().Model(&models.Rendition{ID: "r1", PublicationID: "p1", SocialAccountID: "a1", TargetKey: "a1", Platform: "youtube", Profile: models.ContentProfileLongVideo, Status: models.RenditionStatusDraft, CreatedAt: now, UpdatedAt: now}).Exec(ctx)
	require.NoError(t, err)
}

func TestCreateValidatesOwnershipAndDestination(t *testing.T) {
	db := testDB(t)
	seed(t, db)
	svc := NewService(db)
	_, err := svc.Create(context.Background(), "w1", "p1", "r1", "javascript:alert(1)", "u1")
	require.ErrorIs(t, err, ErrInvalidInput)
	_, err = svc.Create(context.Background(), "w1", "p1", "missing", "https://example.com", "u1")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = svc.Create(context.Background(), "w2", "p1", "", "https://example.com", "u1")
	require.ErrorIs(t, err, ErrNotFound)
	link, err := svc.Create(context.Background(), "w1", "p1", "r1", "https://example.com/repo", "u1")
	require.NoError(t, err)
	require.Len(t, link.Slug, 16)
}

func TestResolveCountsOnlyHumanGet(t *testing.T) {
	db := testDB(t)
	seed(t, db)
	svc := NewService(db)
	link, err := svc.Create(context.Background(), "w1", "p1", "", "https://example.com", "u1")
	require.NoError(t, err)
	now := time.Now().UTC()
	_, counted, err := svc.ResolveAndRecord(context.Background(), link.Slug, http.MethodGet, "Mozilla/5.0", "", now)
	require.NoError(t, err)
	require.True(t, counted)
	_, counted, err = svc.ResolveAndRecord(context.Background(), link.Slug, http.MethodHead, "Mozilla/5.0", "", now)
	require.NoError(t, err)
	require.False(t, counted)
	_, counted, err = svc.ResolveAndRecord(context.Background(), link.Slug, http.MethodGet, "Twitterbot", "", now)
	require.NoError(t, err)
	require.False(t, counted)
	var clicks int
	clicks, err = db.NewSelect().Model((*models.TrackedLinkClick)(nil)).Where("tracked_link_id = ?", link.ID).Count(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, clicks)
}

func TestResolveMissingAndInactive(t *testing.T) {
	db := testDB(t)
	seed(t, db)
	svc := NewService(db)
	_, _, err := svc.ResolveAndRecord(context.Background(), "missing", http.MethodGet, "", "", time.Now())
	require.ErrorIs(t, err, ErrNotFound)
	link, err := svc.Create(context.Background(), "w1", "p1", "", "https://example.com", "u1")
	require.NoError(t, err)
	_, err = db.NewUpdate().Model((*models.TrackedLink)(nil)).Set("is_active = ?", false).Where("id = ?", link.ID).Exec(context.Background())
	require.NoError(t, err)
	_, _, err = svc.ResolveAndRecord(context.Background(), link.Slug, http.MethodGet, "", "", time.Now())
	require.ErrorIs(t, err, ErrInactive)
}
