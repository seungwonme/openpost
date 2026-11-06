package providerinbox

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/uptrace/bun"
)

// MetaApp identifies the app that signed a callback, not merely its provider.
// Instagram Login uses its own Instagram app ID/secret. Facebook Login uses
// the Facebook app ID/secret. Load secrets from the existing credential owner.
type MetaApp struct{ ID, ClientID, Platform, ConnectionMode, AppSecret, VerifyToken string }

type MetaEvent struct {
	Object    string          `json:"object"`
	EntryID   string          `json:"entry_id"`
	Messaging json.RawMessage `json:"messaging,omitempty"`
	Change    json.RawMessage `json:"change,omitempty"`
}

type metaEntry struct {
	ID        string            `json:"id"`
	Messaging []json.RawMessage `json:"messaging"`
	Changes   []json.RawMessage `json:"changes"`
}

type metaReceiptPayload struct {
	kind  string
	event MetaEvent
}

// MetaHandler serves GET verification and signed POST intake at an app-specific
// callback route. No account ID or workspace supplied by the caller is trusted.
func (s *Service) MetaHandler(app MetaApp) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.encryptor == nil || !app.configured() {
			http.Error(w, "Provider callback is not configured", http.StatusServiceUnavailable)
			return
		}
		switch r.Method {
		case http.MethodGet:
			app.verifySubscription(w, r)
		case http.MethodPost:
			s.receiveMeta(w, r, app)
		default:
			w.Header().Set("Allow", "GET, POST")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func (app MetaApp) configured() bool {
	return app.ID != "" && app.ClientID != "" && app.AppSecret != "" && app.VerifyToken != "" && (app.Platform == "facebook" || app.Platform == "instagram")
}

func (app MetaApp) verifySubscription(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if query.Get("hub.mode") != "subscribe" || subtle.ConstantTimeCompare([]byte(query.Get("hub.verify_token")), []byte(app.VerifyToken)) != 1 || len(query.Get("hub.challenge")) > 1024 {
		http.Error(w, "Invalid verification", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, query.Get("hub.challenge"))
}

func (s *Service) receiveMeta(w http.ResponseWriter, r *http.Request, app MetaApp) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		http.Error(w, "Invalid callback body", http.StatusRequestEntityTooLarge)
		return
	}
	digest := hmac.New(sha256.New, []byte(app.AppSecret))
	_, _ = digest.Write(body)
	header := r.Header.Get("X-Hub-Signature-256")
	signature, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil || !strings.HasPrefix(header, "sha256=") || !hmac.Equal(signature, digest.Sum(nil)) {
		http.Error(w, "Invalid signature", http.StatusForbidden)
		return
	}
	if err = s.acceptMeta(r.Context(), app, body); err != nil {
		if errors.Is(err, errInvalidEnvelope) {
			http.Error(w, "Invalid callback payload", http.StatusBadRequest)
		} else {
			http.Error(w, "Callback persistence unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	w.WriteHeader(http.StatusOK)
}

var errInvalidEnvelope = errors.New("invalid Meta callback envelope")

func (s *Service) acceptMeta(ctx context.Context, app MetaApp, body []byte) error {
	var envelope struct {
		Object  string      `json:"object"`
		Entries []metaEntry `json:"entry"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.Entries) > 100 {
		return errInvalidEnvelope
	}
	if (app.Platform == "facebook" && envelope.Object != "page") || (app.Platform == "instagram" && envelope.Object != "instagram") {
		return errInvalidEnvelope
	}
	var rows []receipt
	for _, entry := range envelope.Entries {
		staged, err := s.metaEntryReceipts(ctx, app, envelope.Object, entry)
		if err != nil {
			return err
		}
		rows = append(rows, staged...)
	}
	err := s.db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		for i := range rows {
			if _, err := tx.NewInsert().Model(&rows[i]).On("CONFLICT (provider_app_id, account_id, oauth_grant_id, event_key) DO NOTHING").Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	if s.scheduleReceipts == nil {
		return errors.New("provider receipt scheduler unavailable")
	}
	return s.scheduleReceipts(ctx)
}

func metaEntryPayloads(object string, entry metaEntry) ([]metaReceiptPayload, error) {
	if entry.ID == "" || len(entry.Messaging)+len(entry.Changes) > 1000 {
		return nil, errInvalidEnvelope
	}
	payloads := make([]metaReceiptPayload, 0, len(entry.Messaging)+len(entry.Changes))
	for _, event := range entry.Messaging {
		payloads = append(payloads, metaReceiptPayload{kind: "message", event: MetaEvent{Object: object, EntryID: entry.ID, Messaging: event}})
	}
	for _, event := range entry.Changes {
		var field struct {
			Field string `json:"field"`
		}
		if json.Unmarshal(event, &field) != nil {
			return nil, errInvalidEnvelope
		}
		switch field.Field {
		case "comments", "feed", "live_comments":
			payloads = append(payloads, metaReceiptPayload{kind: "engagement", event: MetaEvent{Object: object, EntryID: entry.ID, Change: event}})
		}
	}
	return payloads, nil
}

func (s *Service) metaEntryReceipts(ctx context.Context, app MetaApp, object string, entry metaEntry) ([]receipt, error) {
	payloads, err := metaEntryPayloads(object, entry)
	if err != nil {
		return nil, err
	}
	accounts, err := s.metaAccounts(ctx, app, entry.ID)
	if err != nil {
		return nil, err
	}
	var rows []receipt
	for _, account := range accounts {
		for _, payload := range payloads {
			if payload.kind == "message" && len(platform.MissingAnalyticsScopes(account.GrantedScopes, app.messagingScopes(account))) > 0 {
				continue
			}
			encoded, err := json.Marshal(payload.event)
			if err != nil {
				return nil, err
			}
			encrypted, err := s.encryptor.Encrypt(string(encoded))
			if err != nil {
				return nil, err
			}
			rows = append(rows, receipt{ID: uuid.NewString(), ProviderAppID: app.ID, AccountID: account.ID, OAuthGrantID: account.OAuthGrantID, Kind: payload.kind, EventKey: eventKey(encoded), PayloadEncrypted: encrypted, CreatedAt: s.now()})
		}
	}
	return rows, nil
}

func (app MetaApp) messagingScopes(account models.SocialAccount) []string {
	if app.Platform == "facebook" {
		return []string{"pages_messaging", "pages_manage_metadata"}
	}
	if platform.AccountProviderKey(account.Platform, account.InstanceURL, account.CapabilityState) == platform.InstagramUnifiedProviderKey {
		return platform.InstagramUnifiedMessagingScopes()
	}
	if app.ConnectionMode == "instagram_login" {
		return []string{"instagram_business_basic", "instagram_business_manage_messages"}
	}
	return []string{"instagram_basic", "instagram_manage_messages", "pages_manage_metadata"}
}

func (s *Service) metaAccounts(ctx context.Context, app MetaApp, remoteID string) ([]models.SocialAccount, error) {
	var accounts []models.SocialAccount
	if err := s.db.NewSelect().Model(&accounts).Where("platform = ? AND account_id = ? AND is_active = ?", app.Platform, remoteID, true).Scan(ctx); err != nil {
		return nil, err
	}
	selected := make([]models.SocialAccount, 0, len(accounts))
	for _, account := range accounts {
		if app.Platform == "instagram" {
			direct := platform.AccountProviderKey(account.Platform, account.InstanceURL, account.CapabilityState) == "instagram:instagram_login"
			if direct != (app.ConnectionMode == "instagram_login") {
				continue
			}
		}
		var grant models.OAuthGrant
		err := s.db.NewSelect().Model(&grant).Where("id = ? AND workspace_id = ? AND provider = ? AND provider_project_id = ? AND revoked_at IS NULL", account.OAuthGrantID, account.WorkspaceID, account.Platform, app.ClientID).Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if grant.ValidationStatus != "valid" {
			continue
		}
		account.GrantedScopes = grant.GrantedScopes
		selected = append(selected, account)
	}
	return selected, nil
}
