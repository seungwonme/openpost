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

func TestTermsPageAndTikTokVerificationFile(t *testing.T) {
	static := filepath.Join("..", "..", "..", "web", "static")
	const verificationName = "tiktokkIio8XHwfOvrD5N1h5XwSPi5Vw824ajM.txt"
	terms, err := os.ReadFile(filepath.Join(static, "terms.html"))
	require.NoError(t, err)
	verification, err := os.ReadFile(filepath.Join(static, verificationName))
	require.NoError(t, err)
	require.Equal(t, "tiktok-developers-site-verification=kIio8XHwfOvrD5N1h5XwSPi5Vw824ajM", string(verification))

	webFS := withAppRoutes(fstest.MapFS{
		"index.html":     {Data: []byte(`<html><head></head><body><div id="app"></div></body></html>`)},
		"terms.html":     {Data: terms},
		verificationName: {Data: verification},
	}, "/")
	e := echo.New()
	registerSpaRoutes(e, webFS)

	termsReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/terms", nil)
	termsRec := httptest.NewRecorder()
	e.ServeHTTP(termsRec, termsReq)
	require.Equal(t, http.StatusOK, termsRec.Code)
	body := termsRec.Body.String()
	for _, want := range []string{`lang="ko"`, `lang="en"`, `href="/privacy"`, "2026-11-08", "https://www.aidenahn.com/about"} {
		require.Contains(t, body, want)
	}
	require.NotContains(t, body, "mailto:")

	txtReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/"+verificationName, nil)
	txtRec := httptest.NewRecorder()
	e.ServeHTTP(txtRec, txtReq)
	require.Equal(t, http.StatusOK, txtRec.Code)
	require.Contains(t, txtRec.Header().Get("Content-Type"), "text/plain")
	require.Equal(t, string(verification), txtRec.Body.String())
}

func TestPagesLinkToEachOther(t *testing.T) {
	static := filepath.Join("..", "..", "..", "web", "static")
	privacy, err := os.ReadFile(filepath.Join(static, "privacy.html"))
	require.NoError(t, err)
	require.Contains(t, string(privacy), `href="/terms"`)
}
