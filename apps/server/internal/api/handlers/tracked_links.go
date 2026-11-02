package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/labstack/echo/v4"
	"github.com/openpost/backend/internal/api/middleware"
	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/services/trackedlinks"
	"github.com/uptrace/bun"
)

type TrackedLinkHandler struct {
	service *trackedlinks.Service
	db      *bun.DB
	auth    middleware.Authenticator
}

func NewTrackedLinkHandler(service *trackedlinks.Service, db *bun.DB, auth middleware.Authenticator) *TrackedLinkHandler {
	return &TrackedLinkHandler{service: service, db: db, auth: auth}
}

type CreateTrackedLinkInput struct {
	WorkspaceID string `path:"workspace_id" doc:"Workspace ID"`
	Body        struct {
		PublicationID  string `json:"publication_id" required:"true" doc:"Publication ID"`
		RenditionID    string `json:"rendition_id,omitempty" doc:"Optional rendition ID"`
		DestinationURL string `json:"destination_url" required:"true" doc:"Redirect destination URL"`
	}
}

type CreateTrackedLinkOutput struct {
	Body models.TrackedLink
}

type ListTrackedLinksInput struct {
	WorkspaceID string `path:"workspace_id" doc:"Workspace ID"`
}

type ListTrackedLinksOutput struct {
	Body []models.TrackedLink
}

func (h *TrackedLinkHandler) RegisterRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "create-tracked-link",
		Method:        http.MethodPost,
		Path:          "/workspaces/{workspace_id}/tracked-links",
		Summary:       "Create a tracked link",
		Tags:          []string{tagPublications},
		DefaultStatus: http.StatusCreated,
		Middlewares:   huma.Middlewares{middleware.AuthMiddleware(api, h.auth)},
		Errors:        []int{400, 403, 404},
	}, func(ctx context.Context, input *CreateTrackedLinkInput) (*CreateTrackedLinkOutput, error) {
		workspaceID := strings.TrimSpace(input.WorkspaceID)
		allowed, err := workspaceEditAllowed(ctx, h.db, workspaceID, middleware.GetUserID(ctx))
		if err != nil {
			return nil, huma.Error500InternalServerError("could not check workspace access")
		}
		if !allowed {
			return nil, huma.Error403Forbidden("workspace edit denied")
		}
		link, err := h.service.Create(
			ctx,
			workspaceID,
			strings.TrimSpace(input.Body.PublicationID),
			strings.TrimSpace(input.Body.RenditionID),
			strings.TrimSpace(input.Body.DestinationURL),
			middleware.GetUserID(ctx),
		)
		if errors.Is(err, trackedlinks.ErrInvalidInput) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		if errors.Is(err, trackedlinks.ErrNotFound) {
			return nil, huma.Error404NotFound("publication or rendition not found")
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("could not create tracked link")
		}
		return &CreateTrackedLinkOutput{Body: link}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-tracked-links",
		Method:      http.MethodGet,
		Path:        "/workspaces/{workspace_id}/tracked-links",
		Summary:     "List tracked links",
		Tags:        []string{tagPublications},
		Middlewares: huma.Middlewares{middleware.AuthMiddleware(api, h.auth)},
		Errors:      []int{403},
	}, func(ctx context.Context, input *ListTrackedLinksInput) (*ListTrackedLinksOutput, error) {
		workspaceID := strings.TrimSpace(input.WorkspaceID)
		allowed, err := workspaceReadAllowed(ctx, h.db, workspaceID, middleware.GetUserID(ctx))
		if err != nil {
			return nil, huma.Error500InternalServerError("could not check workspace access")
		}
		if !allowed {
			return nil, huma.Error403Forbidden("workspace read denied")
		}
		links, err := h.service.List(ctx, workspaceID)
		if err != nil {
			return nil, huma.Error500InternalServerError("could not list tracked links")
		}
		return &ListTrackedLinksOutput{Body: links}, nil
	})
}

func (h *TrackedLinkHandler) RegisterPublicRoutes(e *echo.Echo) {
	e.GET("/go/:slug", h.redirect)
	e.HEAD("/go/:slug", h.redirect)
}

func (h *TrackedLinkHandler) redirect(c echo.Context) error {
	destination, _, err := h.service.ResolveAndRecord(
		c.Request().Context(),
		c.Param("slug"),
		c.Request().Method,
		c.Request().Header.Get("User-Agent"),
		c.Request().Header.Get("Purpose"),
		time.Now().UTC(),
	)
	if destination != "" {
		return c.Redirect(http.StatusFound, destination)
	}
	switch {
	case errors.Is(err, trackedlinks.ErrInvalidInput):
		return echo.NewHTTPError(http.StatusBadRequest, "invalid tracked link slug")
	case errors.Is(err, trackedlinks.ErrNotFound), errors.Is(err, trackedlinks.ErrInactive):
		return echo.NewHTTPError(http.StatusNotFound, "tracked link not found")
	case err != nil:
		return echo.NewHTTPError(http.StatusInternalServerError, "could not resolve tracked link")
	default:
		return echo.NewHTTPError(http.StatusNotFound, "tracked link not found")
	}
}
