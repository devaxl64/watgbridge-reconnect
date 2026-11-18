package database

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func knownChatsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:known-chats-"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil { t.Fatal(err) }
	if err := db.AutoMigrate(&KnownChat{}, &ChatRoute{}); err != nil { t.Fatal(err) }
	return db
}

func TestRecordKnownChatDoesNotDuplicateOrEraseName(t *testing.T) {
	db := knownChatsTestDB(t)
	id := "5511987654321@s.whatsapp.net"
	if err := recordKnownChat(db, KnownChat{WaChatID:id,Name:"Contato",Kind:"private",Source:"history"}); err != nil { t.Fatal(err) }
	if err := recordKnownChat(db, KnownChat{WaChatID:id,Name:"",Kind:"private",Source:"new-message"}); err != nil { t.Fatal(err) }
	var rows []KnownChat
	if err := db.Find(&rows).Error; err != nil { t.Fatal(err) }
	if len(rows)!=1 || rows[0].Name!="Contato" || rows[0].Source!="new-message" {
		t.Fatalf("unexpected catalog: %+v", rows)
	}
}

func TestKnownChatStatsIncludeLinkedOnly(t *testing.T) {
	db := knownChatsTestDB(t)
	now := time.Now()
	for _, chat := range []KnownChat{
		{WaChatID:"5511999999999@s.whatsapp.net",Kind:"private",SeenAt:now},
		{WaChatID:"123@g.us",Kind:"group",SeenAt:now},
	} {
		if err := recordKnownChat(db,chat); err != nil { t.Fatal(err) }
	}
	if err := bindSeparateChat(db, "123@g.us", -100123); err != nil { t.Fatal(err) }
	s,err := getKnownChatStats(db)
	if err != nil { t.Fatal(err) }
	if s.Total!=2 || s.Private!=1 || s.Groups!=1 || s.Linked!=1 {
		t.Fatalf("unexpected stats: %+v",s)
	}
}

func TestRecordKnownChatRejectsInvalid(t *testing.T) {
	db:=knownChatsTestDB(t)
	if err:=recordKnownChat(db,KnownChat{Kind:"private"});err==nil { t.Fatal("blank chat accepted") }
	if err:=recordKnownChat(db,KnownChat{WaChatID:"id",Kind:"broadcast"});err==nil { t.Fatal("broadcast accepted") }
}

func TestPlaceholderZeroChatCannotReenterCatalog(t *testing.T) {
	db := knownChatsTestDB(t)
	for _, id := range []string{"0@s.whatsapp.net", "0@lid"} {
		if err := recordKnownChat(db, KnownChat{WaChatID:id, Kind:"private"}); err == nil {
			t.Fatalf("placeholder %q unexpectedly accepted", id)
		}
		if IsActionableKnownChat(KnownChat{WaChatID:id, Kind:"private"}) {
			t.Fatalf("placeholder %q must never have a dedicated group", id)
		}
	}
}

func TestOldPlaceholderRouteExcludedWithoutDeletingGroup(t *testing.T) {
	db := knownChatsTestDB(t)
	// Emulate a previous deployment that had already created Telegram group "0".
	if err := db.Create(&KnownChat{WaChatID:"0@s.whatsapp.net", Kind:"private", Name:"0"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := bindSeparateChat(db, "0@s.whatsapp.net", -1001234567890); err != nil {
		t.Fatal(err)
	}
	if err := recordKnownChat(db, KnownChat{WaChatID:"5511999999999@s.whatsapp.net", Kind:"private"}); err != nil {
		t.Fatal(err)
	}
	if err := bindSeparateChat(db, "5511999999999@s.whatsapp.net", -1009876543210); err != nil {
		t.Fatal(err)
	}
	stats, err := getKnownChatStats(db)
	if err != nil { t.Fatal(err) }
	if stats.Total != 1 || stats.Linked != 1 || stats.Private != 1 {
		t.Fatalf("old placeholder should be hidden: %+v", stats)
	}
	var existing ChatRoute
	if err := db.Where("wa_chat_id = ?", "0@s.whatsapp.net").First(&existing).Error; err != nil {
		t.Fatal("old Telegram group route should not be deleted automatically:", err)
	}
}
