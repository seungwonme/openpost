package trackedlinks

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openpost/backend/internal/models"
	"github.com/uptrace/bun"
)

var (
	ErrInvalidInput = errors.New("invalid tracked link input")
	ErrNotFound     = errors.New("tracked link not found")
	ErrInactive     = errors.New("tracked link inactive")
)

const (
	slugBytes          = 12
	clickInsertTimeout = 250 * time.Millisecond
)

var slugPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type Service struct{ db *bun.DB }

func NewService(db *bun.DB) *Service { return &Service{db: db} }

func (s *Service) Create(ctx context.Context, workspaceID, publicationID, renditionID, destinationURL, createdByID string) (models.TrackedLink, error) {
	if s == nil || s.db == nil {
		return models.TrackedLink{}, errors.New("tracked link service database is unavailable")
	}
	var link models.TrackedLink
	workspaceID = strings.TrimSpace(workspaceID)
	publicationID = strings.TrimSpace(publicationID)
	renditionID = strings.TrimSpace(renditionID)
	createdByID = strings.TrimSpace(createdByID)
	if workspaceID == "" || publicationID == "" || createdByID == "" {
		return link, fmt.Errorf("%w: workspace, publication, and creator are required", ErrInvalidInput)
	}
	if len(workspaceID) > 200 || len(publicationID) > 200 || len(renditionID) > 200 || len(createdByID) > 200 {
		return link, fmt.Errorf("%w: identifier is too long", ErrInvalidInput)
	}
	destinationURL, err := validateDestination(destinationURL)
	if err != nil {
		return link, err
	}
	var publication models.Publication
	if err := s.db.NewSelect().Model(&publication).Where("id = ? AND workspace_id = ?", publicationID, workspaceID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return link, ErrNotFound
		}
		return link, err
	}
	if renditionID != "" {
		var rendition models.Rendition
		if err := s.db.NewSelect().Model(&rendition).Where("id = ? AND publication_id = ?", renditionID, publication.ID).Scan(ctx); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return link, ErrNotFound
			}
			return link, err
		}
		var accountWorkspaceID string
		if err := s.db.NewSelect().Model((*models.SocialAccount)(nil)).Column("workspace_id").Where("id = ?", rendition.SocialAccountID).Scan(ctx, &accountWorkspaceID); err != nil {
			return link, err
		}
		if accountWorkspaceID != workspaceID {
			return link, ErrNotFound
		}
	}
	for range 5 {
		slug, err := newSlug()
		if err != nil {
			return link, err
		}
		link = models.TrackedLink{ID: uuid.NewString(), WorkspaceID: workspaceID, PublicationID: publicationID, RenditionID: renditionID, Slug: slug, DestinationURL: destinationURL, CreatedByID: createdByID, IsActive: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		_, err = s.db.NewInsert().Model(&link).Exec(ctx)
		if err == nil {
			return link, nil
		}
		if !isUniqueViolation(err) {
			return models.TrackedLink{}, err
		}
	}
	return models.TrackedLink{}, fmt.Errorf("%w: could not allocate unique slug", ErrInvalidInput)
}

func (s *Service) List(ctx context.Context, workspaceID string) ([]models.TrackedLink, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("tracked link service database is unavailable")
	}
	if strings.TrimSpace(workspaceID) == "" {
		return nil, fmt.Errorf("%w: workspace is required", ErrInvalidInput)
	}
	var links []models.TrackedLink
	if err := s.db.NewSelect().Model(&links).Where("workspace_id = ?", workspaceID).Order("created_at DESC").Scan(ctx); err != nil {
		return nil, err
	}
	return links, nil
}

func (s *Service) ResolveAndRecord(ctx context.Context, slug, method, userAgent, purpose string, now time.Time) (string, bool, error) {
	if s == nil || s.db == nil {
		return "", false, errors.New("tracked link service database is unavailable")
	}
	slug = strings.TrimSpace(slug)
	if !slugPattern.MatchString(slug) {
		return "", false, fmt.Errorf("%w: slug is required", ErrInvalidInput)
	}
	var link models.TrackedLink
	if err := s.db.NewSelect().Model(&link).Where("slug = ?", slug).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, ErrNotFound
		}
		return "", false, err
	}
	if !link.IsActive {
		return "", false, ErrInactive
	}
	counted := strings.EqualFold(strings.TrimSpace(method), http.MethodGet) && !obviousBotOrPrefetch(userAgent, purpose)
	if counted {
		insertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), clickInsertTimeout)
		_, err := s.db.NewInsert().Model(&models.TrackedLinkClick{ID: uuid.NewString(), TrackedLinkID: link.ID, OccurredAt: now.UTC()}).Exec(insertCtx)
		cancel()
		_ = err
	}
	return link.DestinationURL, counted, nil
}

func validateDestination(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", fmt.Errorf("%w: destination must be an absolute HTTP or HTTPS URL without credentials", ErrInvalidInput)
	}
	return trimmed, nil
}

func newSlug() (string, error) {
	buf := make([]byte, slugBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func obviousBotOrPrefetch(userAgent, purpose string) bool {
	value := strings.ToLower(strings.TrimSpace(userAgent) + " " + strings.TrimSpace(purpose))
	for _, marker := range []string{"bot", "crawler", "spider", "headless", "preview", "prefetch", "prerender", "slackbot", "facebookexternalhit", "twitterbot", "linkedinbot"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func isUniqueViolation(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") || strings.Contains(message, "constraint")
}
