package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostUpdateUsesPublicationRevision(t *testing.T) {
	t.Setenv("OPENPOST_CONFIG_DIR", t.TempDir())
	var saveBody map[string]any
	saved := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/workspaces":
			_, _ = w.Write([]byte(`[{"id":"ws-1","name":"Production"}]`))
		case "/api/v1/workspaces/ws-1/settings":
			_, _ = w.Write([]byte(`{"timezone":"Europe/Lisbon"}`))
		case "/api/v1/publications/post-1":
			if r.Method == http.MethodGet {
				if saved {
					_, _ = w.Write([]byte(`{"id":"post-1","workspace_id":"ws-1","created_by":"user-1","title":"Updated","content_profile":"short_text","source_text":"Updated","status":"draft","revision":8,"created_at":"2026-07-25T12:00:00Z","updated_at":"2026-07-25T12:00:00Z","renditions":[{"id":"rendition-1","social_account_id":"account-1","platform":"x","profile":"short_text","body":"Updated","status":"pending"}],"media":[]}`))
					return
				}
				_, _ = w.Write([]byte(`{"id":"post-1","workspace_id":"ws-1","created_by":"user-1","title":"Original","content_profile":"short_text","source_text":"Original","status":"draft","revision":7,"created_at":"2026-07-25T12:00:00Z","updated_at":"2026-07-25T12:00:00Z","renditions":[{"id":"rendition-1","social_account_id":"account-1","platform":"x","profile":"short_text","body":"Original","status":"pending"}],"media":[]}`))
				return
			}
			if r.Method != http.MethodPut {
				t.Fatalf("method = %s, want PUT", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&saveBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			saved = true
			_, _ = w.Write([]byte(`{"id":"post-1","workspace_id":"ws-1","created_by":"user-1","title":"Updated","content_profile":"short_text","source_text":"Updated","status":"draft","revision":8,"created_at":"2026-07-25T12:00:00Z","updated_at":"2026-07-25T12:00:00Z","renditions":[{"id":"rendition-1","social_account_id":"account-1","platform":"x","profile":"short_text","body":"Updated","status":"pending"}],"media":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := executeRootCaptureStdout(
		t,
		"--instance", srv.URL,
		"--token", "op_cli_test",
		"--workspace", "Production",
		"post", "update", "post-1",
		"--content", "Updated",
	)
	if err != nil {
		t.Fatalf("post update returned error: %v", err)
	}
	if saveBody["expected_revision"] != float64(7) || saveBody["source_text"] != "Updated" {
		t.Fatalf("save body = %#v", saveBody)
	}
	if !strings.Contains(out, "post-1") || !strings.Contains(out, "draft") {
		t.Fatalf("output = %q", out)
	}
}

func TestResolveMediaInputsPreservesExistingCommaFilenameAndOriginalAltIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launch,final.png")
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := resolveMediaInputs([]string{path}, []string{"Launch card"})
	if len(got) != 1 || got[0].value != path || got[0].alt != "Launch card" || !got[0].localFile {
		t.Fatalf("inputs = %#v", got)
	}
}

func TestResolveMediaInputsExpandsCSVMediaIDsWithoutBorrowingOriginalAlt(t *testing.T) {
	got := resolveMediaInputs([]string{"med_one, med_two"}, []string{"not for expanded IDs"})
	if len(got) != 2 || got[0].value != "med_one" || got[1].value != "med_two" {
		t.Fatalf("inputs = %#v", got)
	}
	if got[0].alt != "" || got[1].alt != "" || got[0].localFile || got[1].localFile {
		t.Fatalf("expanded inputs must remain ID-only without alt text: %#v", got)
	}
}

func TestResolveMediaInputsClassifiesCSVLocalFilesWithoutAlt(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got := resolveMediaInputs([]string{first + "," + second}, []string{"not for expanded files"})
	if len(got) != 2 || got[0].value != first || got[1].value != second {
		t.Fatalf("inputs = %#v", got)
	}
	if !got[0].localFile || !got[1].localFile || got[0].alt != "" || got[1].alt != "" {
		t.Fatalf("expanded local files must not receive alt text: %#v", got)
	}
}

func TestParseThreadMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantFM   threadFrontMatter
		wantBody []string
		wantErr  string
	}{
		{
			name:  "front-matter plus three posts",
			input: "---\nworkspace: personal\naccounts: x,linkedin\nschedule: tomorrow 2pm\nrandom_delay: 7\n---\nOne\n---\nTwo\n---\nThree\n",
			wantFM: threadFrontMatter{
				Workspace:   "personal",
				Accounts:    "x,linkedin",
				Schedule:    "tomorrow 2pm",
				RandomDelay: 7,
			},
			wantBody: []string{"One", "Two", "Three"},
		},
		{
			name:     "no front-matter plus two posts",
			input:    "One\n---\nTwo\n",
			wantBody: []string{"One", "Two"},
		},
		{
			name:     "embedded dashes inside post body",
			input:    "One --- still one\n---\nTwo\n",
			wantBody: []string{"One --- still one", "Two"},
		},
		{
			name:    "empty segment rejected",
			input:   "One\n---\n \n---\nThree\n",
			wantErr: "thread segment 2 is empty",
		},
		{
			name:     "mixed CRLF and LF",
			input:    "One\r\n---\nTwo\r\n---\nThree",
			wantBody: []string{"One", "Two", "Three"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFM, gotBody, err := parseThreadMarkdown(tt.input)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotFM != tt.wantFM {
				t.Fatalf("frontmatter = %#v, want %#v", gotFM, tt.wantFM)
			}
			if strings.Join(gotBody, "\n---\n") != strings.Join(tt.wantBody, "\n---\n") {
				t.Fatalf("segments = %#v, want %#v", gotBody, tt.wantBody)
			}
		})
	}
}

func TestPostCreateScheduleFailureRecovery(t *testing.T) {
	t.Setenv("OPENPOST_CONFIG_DIR", t.TempDir())
	creates, schedules := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/workspaces":
			_, _ = w.Write([]byte(`[{"id":"ws-1","name":"Production"}]`))
		case "GET /api/v1/workspaces/ws-1/settings":
			_, _ = w.Write([]byte(`{"timezone":"Europe/Lisbon"}`))
		case "GET /api/v1/media":
			_, _ = w.Write([]byte(`[]`))
		case "POST /api/v1/publications":
			creates++
			_, _ = w.Write([]byte(`{"id":"post-1","revision":1,"status":"draft"}`))
		case "POST /api/v1/publications/post-1/schedule":
			schedules++
			if schedules == 1 {
				http.Error(w, `{"detail":"queue unavailable"}`, http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"message":"Scheduled","job_id":"job-1"}`))
		case "GET /api/v1/publications/post-1":
			_, _ = w.Write([]byte(`{"id":"post-1","revision":1,"status":"draft"}`))
		case "PUT /api/v1/publications/post-1":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["scheduled_at"] != "2099-01-02T12:00:00Z" {
				t.Errorf("recovery time = %v", body["scheduled_at"])
			}
			_, _ = w.Write([]byte(`{"id":"post-1","revision":2,"status":"draft"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	args := []string{"--instance", srv.URL, "--token", "op_cli_test", "--workspace", "Production"}
	_, err := executeRootCaptureStdout(t, append(args, "post", "create", "--content", "Hello", "--schedule", "2099-01-02T12:00:00Z")...)
	recovery := "openpost post schedule post-1 --at 2099-01-02T12:00:00Z"
	if err == nil || !strings.Contains(err.Error(), "draft created") || !strings.Contains(err.Error(), recovery) || !strings.Contains(err.Error(), "queue unavailable") {
		t.Fatalf("expected partial success and recovery command, got %v", err)
	}
	_, err = executeRootCaptureStdout(t, append(args, strings.Fields(strings.TrimPrefix(recovery, "openpost "))...)...)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	if creates != 1 || schedules != 2 {
		t.Fatalf("creates=%d schedules=%d", creates, schedules)
	}
}

func TestPostCreateReportsAcceptedScheduleWhenStatusLookupFails(t *testing.T) {
	t.Setenv("OPENPOST_CONFIG_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/workspaces":
			_, _ = w.Write([]byte(`[{"id":"ws-1","name":"Production"}]`))
		case "GET /api/v1/workspaces/ws-1/settings":
			_, _ = w.Write([]byte(`{"timezone":"Europe/Lisbon"}`))
		case "GET /api/v1/media":
			_, _ = w.Write([]byte(`[]`))
		case "POST /api/v1/publications":
			_, _ = w.Write([]byte(`{"id":"post-1","revision":1,"status":"draft"}`))
		case "POST /api/v1/publications/post-1/schedule":
			_, _ = w.Write([]byte(`{"message":"Scheduled","job_id":"job-1"}`))
		case "GET /api/v1/publications/post-1":
			http.Error(w, `{"detail":"status unavailable"}`, http.StatusServiceUnavailable)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	_, err := executeRootCaptureStdout(t, "--instance", srv.URL, "--token", "op_cli_test", "--workspace", "Production", "post", "create", "--content", "Hello", "--schedule", "2099-01-02T12:00:00Z")
	if err == nil || !strings.Contains(err.Error(), "created and scheduling accepted") || !strings.Contains(err.Error(), "openpost post view post-1") || !strings.Contains(err.Error(), "status unavailable") {
		t.Fatalf("expected accepted schedule and status recovery command, got %v", err)
	}
}

func TestPostMediaUpdatePreservesMentionedThreadsAndIndependentVariants(t *testing.T) {
	t.Setenv("OPENPOST_CONFIG_DIR", t.TempDir())
	current := `{"id":"post-1","workspace_id":"ws-1","title":"Mentioned thread","source_text":"Hi @alice","content_profile":"thread","status":"draft","revision":7,"segments":[{"id":"source-1","body":"Hi @alice","settings":{"mentions":{"version":1,"spans":[{"start":3,"end":9,"text":"@alice","target":{"provider":"bluesky","kind":"person","id":"did:plc:ewvi7nxzyoun6zhxrhs64oiz"}}]}},"media":[{"id":"old-photo"}]},{"id":"source-2","body":"Keep the thread tail","settings":{},"media":[{"id":"tail-photo"}]}],"renditions":[{"social_account_id":"account-1","platform":"bluesky","body":"Custom @bob","segments":[{"publication_segment_id":"source-1","body_override":"Custom @bob","settings":{"mentions":{"version":1,"spans":[{"start":7,"end":11,"text":"@bob","target":{"provider":"bluesky","kind":"person","id":"did:plc:z72i7hdynmk6r22z27h6tvur"}}]}}}]}]}`
	var saved map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/workspaces":
			_, _ = w.Write([]byte(`[{"id":"ws-1","name":"Production"}]`))
		case "GET /api/v1/workspaces/ws-1/settings":
			_, _ = w.Write([]byte(`{"timezone":"UTC"}`))
		case "GET /api/v1/accounts":
			_, _ = w.Write([]byte(`[{"id":"account-1","platform":"bluesky","account_username":"alice","is_active":true}]`))
		case "GET /api/v1/media":
			_, _ = w.Write([]byte(`{"media":[{"id":"media-2","mime_type":"image/png","url":"https://example.com/image.png"}],"total":1}`))
		case "GET /api/v1/publications/post-1":
			_, _ = w.Write([]byte(current))
		case "PUT /api/v1/publications/post-1":
			if err := json.NewDecoder(r.Body).Decode(&saved); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(current))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	_, err := executeRootCaptureStdout(t, "--instance", srv.URL, "--token", "op_cli_test", "--workspace", "Production", "post", "update", "post-1", "--media", "media-2")
	if err != nil {
		t.Fatal(err)
	}
	segments, ok := saved["segments"].([]any)
	if !ok || len(segments) != 2 {
		t.Fatalf("media updates must preserve canonical thread authorship: %#v", saved)
	}
	first := segments[0].(map[string]any)
	if first["body"] != "Hi @alice" || first["settings"].(map[string]any)["mentions"] == nil {
		t.Fatalf("lost selected identity: %#v", first)
	}
	if first["media"].([]any)[0].(map[string]any)["media_id"] != "media-2" {
		t.Fatalf("first media = %#v", first["media"])
	}
	tail := segments[1].(map[string]any)
	if tail["body"] != "Keep the thread tail" || tail["media"].([]any)[0].(map[string]any)["media_id"] != "tail-photo" {
		t.Fatalf("thread tail changed: %#v", tail)
	}
	if _, present := saved["renditions"]; present {
		t.Fatal("shared attachment edits must retain independent account variants")
	}

	_, err = executeRootCaptureStdout(t, "--instance", srv.URL, "--token", "op_cli_test", "--workspace", "Production", "post", "update", "post-1", "--accounts", "account-1")
	if err != nil {
		t.Fatal(err)
	}
	variant := saved["renditions"].([]any)[0].(map[string]any)
	segment := variant["segments"].([]any)[0].(map[string]any)
	if segment["body_override"] != "Custom @bob" || segment["settings"].(map[string]any)["mentions"] == nil {
		t.Fatalf("account selection lost variant authorship: %#v", variant)
	}
	if _, present := variant["body"]; present {
		t.Fatalf("account selection flattened the variant: %#v", variant)
	}

	current = strings.Replace(current, `"publication_segment_id":"source-1","body_override":"Custom @bob"`, `"id":"stored-segment","publication_segment_id":"source-1","body":"Custom @bob"`, 1)
	var legacy map[string]any
	if err := json.Unmarshal([]byte(current), &legacy); err != nil {
		t.Fatal(err)
	}
	legacy["segments"] = legacy["segments"].([]any)[:1]
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	current = string(encoded)

	for _, extra := range [][]string{nil, {"--content", "Prefix Hi @alice"}} {
		args := append([]string{"--instance", srv.URL, "--token", "op_cli_test", "--workspace", "Production", "post", "update", "post-1", "--accounts", "account-1"}, extra...)
		_, err = executeRootCaptureStdout(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		retained := saved["renditions"].([]any)[0].(map[string]any)["segments"].([]any)[0].(map[string]any)
		if retained["body_override"] != "Custom @bob" || retained["settings"].(map[string]any)["mentions"] == nil {
			t.Fatalf("account selection discarded legacy independent text: %#v", retained)
		}
	}
}

func TestSelectedMentionCommandsUseStructuredContracts(t *testing.T) {
	t.Setenv("OPENPOST_CONFIG_DIR", t.TempDir())
	const segments = `[{"body":"Hi @alice","settings":{"mentions":{"version":1,"spans":[{"start":3,"end":9,"text":"@alice","target":{"provider":"bluesky","kind":"person","id":"did:plc:ewvi7nxzyoun6zhxrhs64oiz"}}]}}},{"body":"Tail"}]`
	path := filepath.Join(t.TempDir(), "segments.json")
	if err := os.WriteFile(path, []byte(segments), 0600); err != nil {
		t.Fatal(err)
	}
	creates := 0
	prepares := 0
	lookups := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/workspaces":
			_, _ = w.Write([]byte(`[{"id":"ws-1","name":"Production"}]`))
		case "GET /api/v1/workspaces/ws-1/settings":
			_, _ = w.Write([]byte(`{"timezone":"UTC"}`))
		case "GET /api/v1/media":
			_, _ = w.Write([]byte(`{"media":[],"total":0}`))
		case "GET /api/v1/accounts":
			_, _ = w.Write([]byte(`[{"id":"account-1","slug":"alice","platform":"bluesky","is_active":true}]`))
		case "GET /api/v1/accounts/account-1/mentions":
			lookups++
			if r.URL.Query().Get("query") != "A & B" {
				t.Errorf("query=%s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"mode":"accounts","candidates":[]}`))
		case "POST /api/v1/publications":
			creates++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			values := body["segments"].([]any)
			if len(values) != 2 || values[0].(map[string]any)["settings"] == nil {
				t.Errorf("lost annotated thread: %#v", body)
			}
			if body["content_profile"] != "thread" {
				t.Errorf("profile=%v", body["content_profile"])
			}
			if destinations, present := body["renditions"].([]any); present {
				for _, entry := range destinations {
					if entry.(map[string]any)["body"] != nil {
						t.Errorf("structured input must inherit all source segments, got variant body %#v", entry)
					}
				}
			}

			_, _ = w.Write([]byte(`{"id":"post-1","revision":1,"status":"draft"}`))
		case "POST /api/v1/media/original/prepare-image":
			prepares++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["account_id"] != "account-1" || body["alt_text"] != "" {
				t.Errorf("prepare=%#v", body)
			}
			_, _ = w.Write([]byte(`{"status":"ready","media":{"id":"fitted","parent_media_id":"original"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	base := []string{"--instance", srv.URL, "--token", "op_cli_test", "--workspace", "Production", "--json"}
	for _, command := range [][]string{{"account", "mentions", "alice", "--query", "A & B"}, {"post", "create", "--segments-file", path, "--accounts", "alice"}, {"publication", "create", "--segments-file", path, "--accounts", "alice"}, {"media", "prepare-image", "original", "--account", "alice", "--output-profile", "bluesky.post", "--alt", ""}} {
		if _, err := executeRootCaptureStdout(t, append(append([]string{}, base...), command...)...); err != nil {
			t.Fatal(err)
		}
	}
	if creates != 2 || prepares != 1 || lookups != 1 {
		t.Fatalf("calls create=%d prepare=%d lookup=%d", creates, prepares, lookups)
	}
}

func TestPostListShowsDashForPostsWithoutASchedule(t *testing.T) {
	t.Setenv("OPENPOST_CONFIG_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/workspaces":
			_, _ = w.Write([]byte(`[{"id":"ws-1","name":"Production"}]`))
		case "/api/v1/workspaces/ws-1/settings":
			_, _ = w.Write([]byte(`{"timezone":"UTC"}`))
		case "/api/v1/publications":
			_, _ = w.Write([]byte(`[
{"id":"published-now","status":"published","source_text":"Went out immediately","actual_run_at":"2026-11-10T09:00:00Z","renditions":[{"id":"r1"}]},
{"id":"plain-draft","status":"draft","source_text":"Not scheduled","scheduled_at":"0001-01-01T00:00:00Z","renditions":[]},
{"id":"queued","status":"scheduled","source_text":"Later","scheduled_at":"2026-11-12T10:00:00Z","renditions":[{"id":"r2"}]}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	out, err := executeRootCaptureStdout(t, "--instance", srv.URL, "--token", "op_cli_test", "--workspace", "Production", "post", "list")
	if err != nil {
		t.Fatal(err)
	}
	lines := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) > 2 {
			lines[fields[0]] = strings.Join(fields[1:3], " ")
		}
	}
	if lines["published-now"] != "published -" {
		t.Errorf("published post without a schedule = %q, want status published and a dash\n%s", lines["published-now"], out)
	}
	if lines["plain-draft"] != "draft -" {
		t.Errorf("unscheduled draft = %q, want status draft and a dash\n%s", lines["plain-draft"], out)
	}
	if lines["queued"] != "scheduled 2026-11-12T10:00:00Z" {
		t.Errorf("scheduled post = %q\n%s", lines["queued"], out)
	}
}
