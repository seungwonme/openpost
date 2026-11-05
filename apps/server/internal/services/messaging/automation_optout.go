package messaging

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/uptrace/bun"
	"golang.org/x/text/unicode/norm"
)

const (
	// Longer messages are about something else. A false positive only silences
	// automation, while a miss keeps messaging someone who refused, so short
	// messages lean toward opting out.
	maxEnglishOptOutWords = 6
	maxKoreanOptOutRunes  = 16
	maxKoreanStopRunes    = 12
)

var (
	englishOptOutPhrases = []string{"unsubscribe", "unsub", "opt out", "optout", "opt me out", "remove me", "leave me alone", "no more messages", "no more dms", "do not message", "do not text", "do not contact", "don t message", "don t text", "don t contact", "dont message", "dont text", "dont contact"}
	englishFillers       = map[string]bool{"please": true, "pls": true, "plz": true, "now": true, "thanks": true, "thank": true, "you": true, "thx": true, "just": true, "ok": true, "okay": true, "it": true, "sir": true, "kindly": true, "already": true, "immediately": true, "right": true}
	stopObjects          = map[string]bool{"replying": true, "reply": true, "messaging": true, "message": true, "messages": true, "msg": true, "msgs": true, "texting": true, "text": true, "sending": true, "send": true, "contacting": true, "contact": true, "dm": true, "dms": true, "dming": true, "spam": true, "spamming": true, "me": true, "to": true, "us": true, "these": true, "them": true, "all": true, "this": true}

	koreanOptOutPhrases = []string{"수신거부", "수신거절", "수신취소", "구독취소", "구독해지", "구독해제", "보내지마", "보내지말", "연락하지마", "연락하지말", "알림꺼", "알림끄"}
	koreanStopWords     = map[string]bool{"스탑": true, "스톱": true, "스탑요": true, "스톱요": true, "거부": true, "거부요": true, "거부합니다": true, "차단": true, "차단요": true, "차단합니다": true}
	// "그만" also begins words that have nothing to do with stopping.
	koreanStopFalseFriends = []string{"그만큼", "그만한", "그만두", "그만둔", "그만뒀", "그만치"}
)

// messageRequestsOptOut recognizes a short message asking automation to stop.
// Compatibility normalization folds full-width letters and decomposed Hangul,
// and splitting on anything that is not a letter or number removes punctuation,
// emoji, variation selectors and joiners. Korean compares without spaces, since
// "그만 해" and "그만해" are the same request.
func messageRequestsOptOut(body string) bool {
	words := strings.FieldsFunc(strings.ToLower(norm.NFKC.String(body)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	if len(words) == 0 {
		return false
	}
	return englishRequestsOptOut(words) || koreanRequestsOptOut(strings.Join(words, ""))
}

func englishRequestsOptOut(words []string) bool {
	if len(words) > maxEnglishOptOutWords {
		return false
	}
	joined := " " + strings.Join(words, " ") + " "
	for _, phrase := range englishOptOutPhrases {
		if strings.Contains(joined, " "+phrase+" ") {
			return true
		}
	}
	rest := make([]string, 0, len(words))
	for _, word := range words {
		if !englishFillers[word] {
			rest = append(rest, word)
		}
	}
	if len(rest) == 0 || (rest[0] != "stop" && rest[0] != "cancel") {
		return false
	}
	for _, word := range rest[1:] {
		if !stopObjects[word] {
			return false
		}
	}
	return true
}

func koreanRequestsOptOut(compact string) bool {
	if koreanStopWords[compact] {
		return true
	}
	length := utf8.RuneCountInString(compact)
	if length > maxKoreanOptOutRunes {
		return false
	}
	for _, phrase := range koreanOptOutPhrases {
		if strings.Contains(compact, phrase) {
			return true
		}
	}
	if length > maxKoreanStopRunes || !strings.Contains(compact, "그만") {
		return false
	}
	for _, word := range koreanStopFalseFriends {
		if strings.Contains(compact, word) {
			return false
		}
	}
	return true
}

// recordMessageOptOut commits with the message that requested it, so no job can
// observe the message without the opt-out. It runs on every path that stores or
// repairs an inbound message, and uses the table comments use.
func (s *Service) recordMessageOptOut(ctx context.Context, db bun.IDB, account models.SocialAccount, conversationID, counterpart string, message platform.ProviderMessage) error {
	if account.Platform != "instagram" || message.Direction != "inbound" || !messageRequestsOptOut(message.Body) {
		return nil
	}
	recorded := false
	for _, author := range []string{message.AuthorRemoteID, counterpart} {
		if author == "" || author == account.AccountID {
			continue
		}
		result, err := db.NewRaw("INSERT INTO engagement_automation_optouts(social_account_id,author_remote_id,created_at) VALUES(?,?,?) ON CONFLICT DO NOTHING", account.ID, author, s.now()).Exec(ctx)
		if err != nil {
			return err
		}
		if rows, err := result.RowsAffected(); err == nil && rows > 0 {
			recorded = true
		}
	}
	tx, ok := db.(bun.Tx)
	if !ok || !recorded {
		return nil
	}
	var conversation models.Conversation
	if err := tx.NewSelect().Model(&conversation).Where("id = ?", conversationID).Scan(ctx); err != nil {
		return err
	}
	return s.cancelConversationRepliesTx(ctx, tx, conversation, stopReasonOptedOut)
}
