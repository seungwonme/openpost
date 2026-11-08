package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// The privacy page is a static file that the SPA handler serves ahead of the
// SPA fallback, so crawlers get the policy text without running JavaScript.
func TestPrivacyPageServedAsStaticHTML(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "privacy.html"))
	require.NoError(t, err)

	webFS := withAppRoutes(fstest.MapFS{
		"index.html":   {Data: []byte(`<html><head></head><body><div id="app"></div></body></html>`)},
		"privacy.html": {Data: page},
	}, "/")
	e := echo.New()
	registerSpaRoutes(e, webFS)

	for _, target := range []string{"/privacy", "/privacy.html"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, target)
		body := rec.Body.String()
		for _, want := range []string{
			`lang="ko"`,
			`lang="en"`,
			`id="data-deletion"`,
			"Google API Services User Data Policy",
			"https://myaccount.google.com/permissions",
			"https://www.aidenahn.com/about",
			"2026-11-08",
			"30",
		} {
			require.Contains(t, body, want, target)
		}
		require.NotContains(t, body, "mailto:", "page must not publish an email address")
	}

	unknown := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/privacy/unknown", nil)
	unknownRec := httptest.NewRecorder()
	e.ServeHTTP(unknownRec, unknown)
	require.Equal(t, http.StatusNotFound, unknownRec.Code)
}
