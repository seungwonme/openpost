package messaging

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/openpost/backend/internal/services/automationoptout"
	"github.com/openpost/backend/internal/services/inboxchanges"
	"github.com/openpost/backend/internal/services/organizationguard"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/uptrace/bun"
	"golang.org/x/text/unicode/norm"
)

type optOutTier int

const (
	optOutNone optOutTier = iota
	// optOutPossible is the word with other content, a question or a negation. It
	// writes no opt-out and hands the conversation to a person.
	optOutPossible
	// optOutDefinite is a refusal with nothing else in it, which is permanent until
	// an operator removes it.
	optOutDefinite
)

const (
	// StopReasonPossibleOptOut is the reason a person sees on a handed-over conversation.
	StopReasonPossibleOptOut = "This message may be an opt-out request, so automation stopped and a person needs to check it."

	// A refusal older than this was about something else, so finding it in history
	// does not create a permanent opt-out. Replaying history must not stop automation
	// for someone who has said nothing about it in a month.
	automationOptOutRecency = 30 * 24 * time.Hour
)

var (
	englishDefiniteOptOuts = englishOptOutCommands()
	englishFillers         = map[string]bool{"please": true, "pls": true, "plz": true, "now": true, "thanks": true, "thank": true, "you": true, "thx": true, "just": true, "ok": true, "okay": true, "it": true, "sir": true, "kindly": true, "already": true, "immediately": true, "right": true}
	// stopActions name what to stop. `stop this` or `stop all` could mean anything, so
	// they go to a person. The docs list these words.
	stopActions = map[string]bool{"replying": true, "reply": true, "messaging": true, "message": true, "messages": true, "msg": true, "msgs": true, "texting": true, "texts": true, "sending": true, "send": true, "contacting": true, "contact": true, "dm": true, "dms": true, "dming": true, "spam": true, "spamming": true}
	// A message that is only `cancel` and these does not say what to cancel. Any other
	// word names something, such as an order, so it is ordinary business.
	cancelCompanions = map[string]bool{"this": true, "that": true, "all": true, "me": true, "my": true, "subscription": true, "message": true, "messages": true, "msg": true, "msgs": true, "dm": true, "dms": true, "text": true, "texts": true}
	// These are said to a person who is not being asked to stop anything.
	englishOrdinaryStops   = []string{" stop by ", " don t stop ", " dont stop ", " do not stop ", " can t stop ", " cant stop ", " cannot stop ", " non stop ", " bus stop ", " stop sign "}
	englishPossibleWords   = map[string]bool{"stop": true, "unsubscribe": true, "unsub": true, "optout": true}
	englishPossiblePhrases = []string{"opt out", "opt me out", "remove me", "leave me alone", "no more messages", "no more dms", "do not message", "do not text", "do not contact", "do not dm", "don t message", "don t text", "don t contact", "don t dm", "dont message", "dont text", "dont contact", "dont dm"}

	koreanDefiniteOptOuts = koreanOptOutCommands()
	koreanFillers         = []string{"제발", "이제", "좀"}
	koreanObjects         = []string{"메시지", "메세지", "문자", "디엠", "dm", "연락"}
	koreanPossiblePhrases = []string{"수신거부", "수신거절", "수신취소", "구독취소", "구독해지", "구독해제", "보내지마", "보내지말", "연락하지마", "연락하지말", "알림꺼", "알림끄"}
	// These count only as the whole message: `카드 거부` and `차단 해제 어떻게 해요` are not about automation.
	koreanPossibleWords = map[string]bool{"거부": true, "거부요": true, "거부합니다": true, "거부할게요": true, "차단": true, "차단요": true, "차단합니다": true, "차단할게요": true}
	// These report a phone setting. They ask for nothing.
	koreanNotificationStates = []string{"알림꺼졌", "알림꺼져", "알림꺼짐", "알림꺼놨", "알림꺼놓", "알림꺼뒀", "알림꺼둔", "알림꺼둘", "알림꺼두", "알림꺼서"}
	koreanStopWords          = []string{"스탑", "스톱"}
	// "그만" also begins words that have nothing to do with stopping.
	koreanStopFalseFriends = []string{"그만큼", "그만한", "그만두", "그만둔", "그만뒀", "그만치"}
)

func englishOptOutCommands() map[string]bool {
	commands := map[string]bool{}
	for _, command := range []string{"unsubscribe", "unsubscribe me", "unsub", "unsub me", "optout", "opt out", "opt me out", "remove me", "leave me alone", "no more messages", "no more dms"} {
		commands[command] = true
	}
	for _, negation := range []string{"do not", "don t", "dont"} {
		for _, verb := range []string{"message", "text", "contact", "dm"} {
			for _, object := range []string{"", " me", " us"} {
				commands[negation+" "+verb+object] = true
			}
		}
	}
	return commands
}

func koreanOptOutCommands() map[string]bool {
	commands := map[string]bool{}
	add := func(stems, endings []string) {
		for _, stem := range stems {
			for _, ending := range endings {
				commands[stem+ending] = true
			}
		}
	}
	add([]string{"그만"}, []string{"", "요", "해", "해요", "해라", "해줘", "해줘요", "해주세요", "하세요", "하십시오", "보내", "보내요", "보내라", "보내줘", "보내줘요", "보내세요", "보내주세요", "보내십시오"})
	add([]string{"수신거부", "수신거절", "수신취소", "구독취소", "구독해지", "구독해제"}, []string{"", "요", "합니다", "할게요", "해요", "해주세요", "해줘"})
	add([]string{"보내지", "연락하지", "문자하지", "디엠하지"}, []string{"마", "마요", "마세요", "마십시오", "마라", "말아요", "말아주세요", "말아줘", "말아줘요"})
	add(koreanStopWords, []string{"", "요", "해주세요"})
	return commands
}

// classifyOptOut sorts a message by how clearly it refuses automation. A definite
// opt-out is permanent and also stops comment automation, so only a refusal with
// nothing else in it qualifies. Anything else that mentions stopping goes to a
// person, who can tell "수신거부 어떻게 하나요" from a real request. Compatibility
// normalization folds full-width letters and decomposed Hangul, and splitting on
// anything that is not a letter or number removes punctuation, emoji, variation
// selectors and joiners. Korean compares without spaces.
func classifyOptOut(body string) optOutTier {
	normalized := strings.ToLower(norm.NFKC.String(body))
	words := strings.FieldsFunc(normalized, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	if len(words) == 0 {
		return optOutNone
	}
	compact := strings.Join(words, "")
	if !strings.Contains(normalized, "?") && (englishRefuses(words) || koreanRefuses(compact)) {
		return optOutDefinite
	}
	if englishMentionsStopping(words) || koreanMentionsStopping(words, compact) {
		return optOutPossible
	}
	return optOutNone
}

func englishRefuses(words []string) bool {
	rest := make([]string, 0, len(words))
	for _, word := range words {
		if !englishFillers[word] {
			rest = append(rest, word)
		}
	}
	if len(rest) == 0 {
		return false
	}
	if rest[0] == "stop" {
		return stopNamesMessaging(rest[1:])
	}
	return englishDefiniteOptOuts[strings.Join(rest, " ")]
}

// stopNamesMessaging accepts `stop` alone, or followed by what to stop, which `me`
// or `us` can then follow.
func stopNamesMessaging(words []string) bool {
	if len(words) == 0 {
		return true
	}
	if !stopActions[words[0]] {
		return false
	}
	for _, word := range words[1:] {
		if !stopActions[word] && word != "me" && word != "us" {
			return false
		}
	}
	return true
}

func englishBareCancel(words []string) bool {
	found := false
	for _, word := range words {
		switch {
		case word == "cancel":
			found = true
		case !englishFillers[word] && !cancelCompanions[word]:
			return false
		}
	}
	return found
}

func englishMentionsStopping(words []string) bool {
	if englishBareCancel(words) {
		return true
	}
	joined := " " + strings.Join(words, " ") + " "
	for _, phrase := range englishPossiblePhrases {
		if strings.Contains(joined, " "+phrase+" ") {
			return true
		}
	}
	for _, ordinary := range englishOrdinaryStops {
		joined = strings.ReplaceAll(joined, ordinary, " ")
	}
	for _, word := range strings.Fields(joined) {
		if englishPossibleWords[word] {
			return true
		}
	}
	return false
}

func withoutKoreanFillers(compact string) string {
	for _, filler := range koreanFillers {
		compact = strings.ReplaceAll(compact, filler, "")
	}
	return compact
}

func koreanRefuses(compact string) bool {
	compact = withoutKoreanFillers(compact)
	if koreanDefiniteOptOuts[compact] {
		return true
	}
	for _, object := range koreanObjects {
		if rest, ok := strings.CutPrefix(compact, object); ok && koreanDefiniteOptOuts[rest] {
			return true
		}
	}
	return false
}

func koreanMentionsStopping(words []string, compact string) bool {
	if koreanPossibleWords[withoutKoreanFillers(compact)] {
		return true
	}
	for _, state := range koreanNotificationStates {
		compact = strings.ReplaceAll(compact, state, "")
	}
	for _, phrase := range koreanPossiblePhrases {
		if strings.Contains(compact, phrase) {
			return true
		}
	}
	for _, word := range words {
		for _, stop := range koreanStopWords {
			if strings.HasPrefix(word, stop) {
				return true
			}
		}
	}
	for _, friend := range koreanStopFalseFriends {
		compact = strings.ReplaceAll(compact, friend, "")
	}
	return strings.Contains(compact, "그만")
}

// recordMessageOptOut commits with the message, so no job can observe the message
// without its effect. It runs on every path that stores or repairs an inbound
// message. A definite opt-out uses the table comments use, unless the message is
// older than automationOptOutRecency. A possible one writes nothing to that table
// and hands the conversation to a person.
func (s *Service) recordMessageOptOut(ctx context.Context, db bun.IDB, account models.SocialAccount, conversationID, counterpart string, message platform.ProviderMessage) error {
	if account.Platform != "instagram" || message.Direction != "inbound" {
		return nil
	}
	tx, ok := db.(bun.Tx)
	switch tier := classifyOptOut(message.Body); {
	case tier == optOutPossible && ok:
		return s.handOffPossibleOptOut(ctx, tx, account, conversationID, counterpart, message)
	case tier != optOutDefinite:
		return nil
	}
	recorded, err := s.recordDefiniteOptOut(ctx, db, account, counterpart, message)
	if err != nil || !ok || !recorded {
		return err
	}
	var conversation models.Conversation
	if err := tx.NewSelect().Model(&conversation).Where("id = ?", conversationID).Scan(ctx); err != nil {
		return err
	}
	return s.cancelConversationRepliesTx(ctx, tx, conversation, stopReasonOptedOut)
}

func (s *Service) recordDefiniteOptOut(ctx context.Context, db bun.IDB, account models.SocialAccount, counterpart string, message platform.ProviderMessage) (bool, error) {
	if !message.RemoteCreatedAt.IsZero() && s.now().Sub(message.RemoteCreatedAt) > automationOptOutRecency {
		return false, nil
	}
	recorded := false
	for _, author := range []string{message.AuthorRemoteID, counterpart} {
		if author == "" || author == account.AccountID {
			continue
		}
		inserted, err := automationoptout.Record(ctx, db, account.ID, author, message.RemoteCreatedAt, s.now())
		if err != nil {
			return false, err
		}
		recorded = recorded || inserted
	}
	return recorded, nil
}

// handOffPossibleOptOut stops automation in this conversation only. It does not
// lapse, because the person may have refused. An old message cannot be answering
// anything current, and one message hands over once, so a refresh after Resume
// does not stop the conversation again.
func (s *Service) handOffPossibleOptOut(ctx context.Context, tx bun.Tx, account models.SocialAccount, conversationID, counterpart string, message platform.ProviderMessage) error {
	if message.AuthorRemoteID == account.AccountID {
		return nil
	}
	if !message.RemoteCreatedAt.IsZero() && s.now().Sub(message.RemoteCreatedAt) > automationTakeoverRecency {
		return nil
	}
	optedOut, err := tx.NewSelect().Table("engagement_automation_optouts").Where("social_account_id = ? AND author_remote_id = ?", account.ID, counterpart).Exists(ctx)
	if err != nil || optedOut {
		return err
	}
	var stored models.DirectMessage
	if err := tx.NewSelect().Model(&stored).Where("conversation_id = ? AND remote_message_id = ?", conversationID, message.ID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	result, err := tx.NewRaw("INSERT INTO message_optout_reviews(message_id,created_at) VALUES(?,?) ON CONFLICT DO NOTHING", stored.ID, s.now()).Exec(ctx)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil || rows == 0 {
		return err
	}
	var conversation models.Conversation
	if err := tx.NewSelect().Model(&conversation).Where("id = ?", conversationID).Scan(ctx); err != nil {
		return err
	}
	return s.stopConversationTx(ctx, tx, conversation, "provider", StopReasonPossibleOptOut, time.Time{})
}

// RemoveConversationOptOut is the operator decision that this person may receive
// automation again, on comments and direct messages. It starts nothing by itself,
// and a takeover in the conversation stays until someone resumes.
func (s *Service) RemoveConversationOptOut(ctx context.Context, actor Actor, workspaceID, conversationID string) error {
	if err := s.authorize(ctx, workspaceID, actor, workspaceaccess.LevelEdit); err != nil {
		return err
	}
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := organizationguard.LockWorkspace(ctx, tx, workspaceID); err != nil {
			return err
		}
		var conversation models.Conversation
		if err := tx.NewSelect().Model(&conversation).Where("id = ? AND workspace_id = ?", conversationID, workspaceID).Scan(ctx); err != nil {
			return ErrNotFound
		}
		removed, err := automationoptout.Remove(ctx, tx, workspaceID, conversation.SocialAccountID, conversation.CounterpartRemoteID, actor.UserID, s.now())
		if err != nil || !removed {
			return err
		}
		return inboxchanges.Append(ctx, tx, workspaceID, inboxchanges.Change{Kind: "conversation", ConversationID: conversationID})
	})
}
