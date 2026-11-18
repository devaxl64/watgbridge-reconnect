package database

import (
	"errors"
	"fmt"
	"strings"

	"watgbridge/state"

	"gorm.io/gorm"
)

// ErrChatAlreadyLinked means the Telegram group is already bound to another
// WhatsApp chat. We refuse to silently merge private conversations.
var ErrChatAlreadyLinked = errors.New("telegram chat already linked to another WhatsApp chat")

// BindSeparateChat stores a one-to-one association between a WhatsApp JID and
// its dedicated Telegram group. The group must already exist; the Bot API
// cannot create groups on behalf of a user.
func BindSeparateChat(waChatID string, tgChatID int64) error {
	return bindSeparateChat(state.State.Database, waChatID, tgChatID)
}

func bindSeparateChat(db *gorm.DB, waChatID string, tgChatID int64) error {
	waChatID = strings.TrimSpace(waChatID)
	if db == nil {
		return errors.New("database is not initialized")
	}
	if waChatID == "" || tgChatID == 0 {
		return errors.New("both WhatsApp chat ID and Telegram chat ID are required")
	}

	return db.Transaction(func(tx *gorm.DB) error {
		var existing ChatRoute
		err := tx.Where("tg_chat_id = ?", tgChatID).First(&existing).Error
		if err == nil && existing.WaChatID != waChatID {
			return fmt.Errorf("%w: %d", ErrChatAlreadyLinked, tgChatID)
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Save(&ChatRoute{WaChatID: waChatID, TgChatID: tgChatID}).Error
	})
}

// TelegramGroupForWhatsApp returns the dedicated Telegram group, if linked.
func TelegramGroupForWhatsApp(waChatID string) (int64, bool, error) {
	return telegramGroupForWhatsApp(state.State.Database, waChatID)
}

func telegramGroupForWhatsApp(db *gorm.DB, waChatID string) (int64, bool, error) {
	if db == nil {
		return 0, false, errors.New("database is not initialized")
	}
	var route ChatRoute
	err := db.Where("wa_chat_id = ?", strings.TrimSpace(waChatID)).First(&route).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return route.TgChatID, true, nil
}

// WhatsAppChatForTelegramGroup resolves replies from a dedicated Telegram group.
func WhatsAppChatForTelegramGroup(tgChatID int64) (string, bool, error) {
	return whatsAppChatForTelegramGroup(state.State.Database, tgChatID)
}

func whatsAppChatForTelegramGroup(db *gorm.DB, tgChatID int64) (string, bool, error) {
	if db == nil {
		return "", false, errors.New("database is not initialized")
	}
	var route ChatRoute
	err := db.Where("tg_chat_id = ?", tgChatID).First(&route).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return route.WaChatID, true, nil
}

// UnbindSeparateChat removes a pairing; it does not delete the Telegram group.
func UnbindSeparateChat(waChatID string) error {
	if state.State.Database == nil {
		return errors.New("database is not initialized")
	}
	return state.State.Database.Where("wa_chat_id = ?", strings.TrimSpace(waChatID)).Delete(&ChatRoute{}).Error
}
