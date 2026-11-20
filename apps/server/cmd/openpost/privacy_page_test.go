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
			"7일",
			"within 7 days",
			"TikTok",
			`href="/about"`,
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
	terms, err := os.ReadFile(filepath.Join(static, "terms.html"))
	require.NoError(t, err)

	verificationFiles := map[string]string{
		"tiktokkIio8XHwfOvrD5N1h5XwSPi5Vw824ajM.txt": "tiktok-developers-site-verification=kIio8XHwfOvrD5N1h5XwSPi5Vw824ajM",
		"tiktokGvky3zADrNAANClBnIu3fXarJcPaGoSa.txt": "tiktok-developers-site-verification=Gvky3zADrNAANClBnIu3fXarJcPaGoSa",
		"tiktok4UuSDPdOEw0kxnPejH0hzTN6zOFnSo3V.txt": "tiktok-developers-site-verification=4UuSDPdOEw0kxnPejH0hzTN6zOFnSo3V",
	}
	files := fstest.MapFS{
		"index.html": {Data: []byte(`<html><head></head><body><div id="app"></div></body></html>`)},
		"terms.html": {Data: terms},
	}
	for name, want := range verificationFiles {
		data, err := os.ReadFile(filepath.Join(static, name))
		require.NoError(t, err)
		require.Equal(t, want, string(data), name)
		files[name] = &fstest.MapFile{Data: data}
	}
	e := echo.New()
	registerSpaRoutes(e, withAppRoutes(files, "/"))

	termsReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/terms", nil)
	termsRec := httptest.NewRecorder()
	e.ServeHTTP(termsRec, termsReq)
	require.Equal(t, http.StatusOK, termsRec.Code)
	body := termsRec.Body.String()
	for _, want := range []string{`lang="ko"`, `lang="en"`, `href="/privacy"`, "2026-11-08", "https://www.aidenahn.com/about"} {
		require.Contains(t, body, want)
	}
	require.NotContains(t, body, "mailto:")

	for name, want := range verificationFiles {
		txtReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/"+name, nil)
		txtRec := httptest.NewRecorder()
		e.ServeHTTP(txtRec, txtReq)
		require.Equal(t, http.StatusOK, txtRec.Code, name)
		require.Contains(t, txtRec.Header().Get("Content-Type"), "text/plain", name)
		require.Equal(t, want, txtRec.Body.String(), name)
	}
}

func TestPagesLinkToEachOther(t *testing.T) {
	static := filepath.Join("..", "..", "..", "web", "static")
	privacy, err := os.ReadFile(filepath.Join(static, "privacy.html"))
	require.NoError(t, err)
	require.Contains(t, string(privacy), `href="/terms"`)
	require.Contains(t, string(privacy), `href="/about"`)
}

// The about page is the public homepage that Google's app verification reads:
// it must describe the app and link the policy pages, not only offer sign-in.
func TestAboutPageServedAsStaticHTML(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "about.html"))
	require.NoError(t, err)

	e := echo.New()
	registerSpaRoutes(e, withAppRoutes(fstest.MapFS{
		"index.html": {Data: []byte(`<html><head></head><body><div id="app"></div></body></html>`)},
		"about.html": {Data: page},
	}, "/"))

	for _, target := range []string{"/about", "/about.html"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, target)
		body := rec.Body.String()
		for _, want := range []string{
			"<title>OpenPost 소개 / About OpenPost | post.aidenahn.com</title>",
			`lang="ko"`,
			`lang="en"`,
			`href="/privacy"`,
			`href="/terms"`,
			`href="/"`,
			"https://github.com/getopenpost/openpost",
			"https://www.aidenahn.com/about",
			"youtube.upload",
			"youtube.force-ssl",
			"yt-analytics.readonly",
			"Google API Services User Data Policy",
		} {
			require.Contains(t, body, want, target)
		}
		// Scopes the app no longer requests must not be advertised.
		for _, dropped := range []string{"<code>youtube</code>", "<code>youtube.readonly</code>"} {
			require.NotContains(t, body, dropped, target)
		}
		require.NotContains(t, body, "mailto:", "page must not publish an email address")
	}
}

// TikTok app review compares the app name and icon with the website: every
// public page must name OpenPost and link the same icon as the browser tab.
func TestPublicPagesShareOpenPostNameAndIcon(t *testing.T) {
	static := filepath.Join("..", "..", "..", "web", "static")
	favicon, err := os.ReadFile(filepath.Join(static, "favicon.ico"))
	require.NoError(t, err)

	files := fstest.MapFS{
		"index.html":  {Data: []byte(`<html><head><title>OpenPost</title></head><body><div id="app"></div></body></html>`)},
		"favicon.ico": {Data: favicon},
	}
	for _, name := range []string{"about.html", "privacy.html", "terms.html"} {
		page, err := os.ReadFile(filepath.Join(static, name))
		require.NoError(t, err)
		files[name] = &fstest.MapFile{Data: page}
		for _, want := range []string{
			`<link rel="icon" href="/favicon.ico" sizes="any">`,
			`href="/assets/brand/icon.svg"`,
			`rel="apple-touch-icon" href="/assets/brand/apple-touch-icon.png"`,
		} {
			require.Contains(t, string(page), want, name)
		}
	}
	e := echo.New()
	registerSpaRoutes(e, withAppRoutes(files, "/", "/login"))

	for _, target := range []string{"/", "/login", "/about", "/privacy", "/terms"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, target)
		body := rec.Body.String()
		require.Regexp(t, `<title>[^<]*OpenPost[^<]*</title>`, body, target)
	}
	for _, target := range []string{"/", "/login"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		require.Contains(t, rec.Body.String(), "<h1 style=\"font-size:2rem;line-height:1.2\">OpenPost</h1>", target)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/favicon.ico", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "image/")
	require.Equal(t, favicon, rec.Body.Bytes())
}
