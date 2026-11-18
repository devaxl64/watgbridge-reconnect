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
