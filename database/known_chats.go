package database

import (
	"errors"
	"strings"
	"time"

	"watgbridge/state"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// KnownChat contains metadata only. Message text and attachments are never
// stored in this catalog. Telegram group IDs remain in ChatRoute separately.
type KnownChat struct {
	WaChatID string    `gorm:"primaryKey;column:wa_chat_id"`
	Name     string    `gorm:"column:name"`
	Kind     string    `gorm:"column:kind;index"`
	Source   string    `gorm:"column:source"`
	SeenAt   time.Time `gorm:"column:seen_at;index"`
	PictureID string `gorm:"column:picture_id"`
	PictureCheckedAt time.Time `gorm:"column:picture_checked_at"`
}

func validKnownChat(chat KnownChat) bool {
	return strings.TrimSpace(chat.WaChatID) != "" &&
		(chat.Kind == "group" || chat.Kind == "private")
}

// RecordKnownChat upserts by the stable WhatsApp JID. It never clears an
// existing display name when an event does not have one.
func RecordKnownChat(chat KnownChat) error {
	return recordKnownChat(state.State.Database, chat)
}

func recordKnownChat(db *gorm.DB, chat KnownChat) error {
	if db == nil {
		return errors.New("bridge database is not ready")
	}
	chat.WaChatID = strings.TrimSpace(chat.WaChatID)
	chat.Name = strings.TrimSpace(chat.Name)
	if !validKnownChat(chat) {
		return errors.New("invalid chat ID or chat type")
	}
	if chat.SeenAt.IsZero() {
		chat.SeenAt = time.Now().UTC()
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "wa_chat_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"kind": chat.Kind,
			"seen_at": chat.SeenAt,
			// A blank event name must never erase a known contact name.
			"name": gorm.Expr("CASE WHEN excluded.name <> '' THEN excluded.name ELSE known_chats.name END"),
			"source": chat.Source,
		}),
	}).Create(&chat).Error
}

type KnownChatStats struct {
	Total int64
	Groups int64
	Private int64
	Linked int64
}

// GetKnownChatStats reports how many WhatsApp chats have been discovered
// and how many already have Telegram groups mapped. No media is involved.
func GetKnownChatStats() (KnownChatStats, error) {
	return getKnownChatStats(state.State.Database)
}

func getKnownChatStats(db *gorm.DB) (KnownChatStats, error) {
	var out KnownChatStats
	if db == nil {
		return out, errors.New("bridge database is not ready")
	}
	if err := db.Model(&KnownChat{}).Count(&out.Total).Error; err != nil {
		return out, err
	}
	if err := db.Model(&KnownChat{}).Where("kind = ?", "group").Count(&out.Groups).Error; err != nil {
		return out, err
	}
	if err := db.Model(&KnownChat{}).Where("kind = ?", "private").Count(&out.Private).Error; err != nil {
		return out, err
	}
	if err := db.Model(&KnownChat{}).
		Joins("JOIN chat_routes ON known_chats.wa_chat_id = chat_routes.wa_chat_id").
		Count(&out.Linked).Error; err != nil {
		return out, err
	}
	return out, nil
}

// ListKnownChats allows the owner to inspect a small catalog page without
// exposing any history, media, or private content in logs.
func ListKnownChats(limit int) ([]KnownChat, error) {
	if state.State.Database == nil {
		return nil, errors.New("bridge database is not ready")
	}
	if limit < 1 || limit > 50 {
		limit = 20
	}
	var chats []KnownChat
	err := state.State.Database.Order("seen_at DESC").Order("wa_chat_id ASC").Limit(limit).Find(&chats).Error
	return chats, err
}

func KnownAvatarVersion(waChatID string) (string, time.Time, error) {
	if state.State.Database == nil {
		return "", time.Time{}, errors.New("database is not initialized")
	}
	var chat KnownChat
	err := state.State.Database.Where("wa_chat_id = ?", waChatID).First(&chat).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", time.Time{}, nil
	}
	if err != nil {
		return "", time.Time{}, err
	}
	return chat.PictureID, chat.PictureCheckedAt, nil
}

func SetKnownAvatarVersion(waChatID, pictureID string) error {
	if state.State.Database == nil {
		return errors.New("database is not initialized")
	}
	return state.State.Database.Model(&KnownChat{}).
		Where("wa_chat_id = ?", waChatID).
		Updates(map[string]any{
			"picture_id": pictureID,
			"picture_checked_at": time.Now().UTC(),
		}).Error
}

// ListAllKnownChats returns a bounded batch in a stable order for MTProto
// provisioning. It does not expose historical messages or media.
func ListAllKnownChats(limit int) ([]KnownChat,error) {
	if state.State.Database==nil {return nil,errors.New("bridge database unavailable")}
	if limit<=0||limit>5000 {limit=5000}
	var out []KnownChat
	err:=state.State.Database.Order("wa_chat_id ASC").Limit(limit).Find(&out).Error
	return out,err
}
