package database

import (
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newRouteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&ChatRoute{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSeparateChatRoundTrip(t *testing.T) {
	db := newRouteTestDB(t)
	const jid = "5511999999999@s.whatsapp.net"
	const tgID int64 = -1003987753765

	if err := bindSeparateChat(db, jid, tgID); err != nil {
		t.Fatal(err)
	}
	group, found, err := telegramGroupForWhatsApp(db, jid)
	if err != nil || !found || group != tgID {
		t.Fatalf("forward mapping = (%d, %v, %v)", group, found, err)
	}
	wa, found, err := whatsAppChatForTelegramGroup(db, tgID)
	if err != nil || !found || wa != jid {
		t.Fatalf("reverse mapping = (%q, %v, %v)", wa, found, err)
	}
}

func TestSeparateChatsNeverShareTelegramGroup(t *testing.T) {
	db := newRouteTestDB(t)
	if err := bindSeparateChat(db, "first@g.us", -100111); err != nil {
		t.Fatal(err)
	}
	if err := bindSeparateChat(db, "second@g.us", -100111); !errors.Is(err, ErrChatAlreadyLinked) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestSeparateChatCanMoveToAnotherGroup(t *testing.T) {
	db := newRouteTestDB(t)
	const jid = "first@g.us"
	if err := bindSeparateChat(db, jid, -100111); err != nil {
		t.Fatal(err)
	}
	if err := bindSeparateChat(db, jid, -100222); err != nil {
		t.Fatal(err)
	}
	wa, found, err := whatsAppChatForTelegramGroup(db, -100111)
	if err != nil || found || wa != "" {
		t.Fatalf("stale reverse mapping = (%q, %v, %v)", wa, found, err)
	}
	id, found, err := telegramGroupForWhatsApp(db, jid)
	if err != nil || !found || id != -100222 {
		t.Fatalf("new mapping = (%d, %v, %v)", id, found, err)
	}
}

func TestSeparateChatRejectsInvalidIDs(t *testing.T) {
	db := newRouteTestDB(t)
	if err := bindSeparateChat(db, "", -100111); err == nil {
		t.Fatal("accepted empty WhatsApp ID")
	}
	if err := bindSeparateChat(db, "first@g.us", 0); err == nil {
		t.Fatal("accepted empty Telegram chat ID")
	}
}
