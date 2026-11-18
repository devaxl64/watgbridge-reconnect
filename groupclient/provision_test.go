package groupclient

import (
	"testing"

	"watgbridge/database"
)

func TestChatTitlesWithoutEmojis(t *testing.T){
	tests:=[]struct{in database.KnownChat;want string}{
		{database.KnownChat{WaChatID:"5511999999999@s.whatsapp.net",Name:"João",Kind:"private"},"João (+5511999999999)"},
		{database.KnownChat{WaChatID:"123@g.us",Name:"Amigos",Kind:"group"},"Amigos"},
		{database.KnownChat{WaChatID:"5511888888888@s.whatsapp.net",Kind:"private"},"+5511888888888"},
	}
	for _,c:=range tests{
		if got:=chatTitle(c.in);got!=c.want {t.Errorf("title: %q, want %q",got,c.want)}
	}
}
func TestSupergroupID(t *testing.T) {
	if got:=SupergroupID(3987753765);got!=-1003987753765{t.Errorf("wrong supergroup ID: %d",got)}
}

func TestChatTitleDoesNotMistakeDatabaseIDsForContactNames(t *testing.T) {
	tests := []struct {
		chat database.KnownChat
		want string
	}{
		{database.KnownChat{WaChatID:"15557420242@s.whatsapp.net",Kind:"private",Name:"15557420242"}, "+15557420242"},
		{database.KnownChat{WaChatID:"5511987654321@s.whatsapp.net",Kind:"private",Name:"11987654321"}, "+5511987654321"},
		{database.KnownChat{WaChatID:"5511987654321@s.whatsapp.net",Kind:"private",Name:"Maria"}, "Maria (+5511987654321)"},
	}
	for _, tc := range tests {
		if got := chatTitle(tc.chat); got != tc.want {
			t.Errorf("chatTitle(%q): got %q, want %q", tc.chat.WaChatID, got, tc.want)
		}
	}
}

func TestHumanContactName(t *testing.T) {
	for _, name := range []string{"", "15557420242", "+15557420242", "0"} {
		if humanContactName(name, "15557420242") {
			t.Errorf("raw identifier %q was treated as a saved name", name)
		}
	}
	if !humanContactName("João Silva", "15557420242") {
		t.Fatal("actual contact name not accepted")
	}
}

func TestLegacyNumericGroupTitleOnly(t *testing.T) {
	for _, tc := range []struct{title string; want bool}{
		{"15557420242",true},
		{"+15557420242",true},
		{"João (+15557420242)",false},
		{"Grupo da família",false},
	} {
		if got:=isLegacyNumericTitle(tc.title,"15557420242");got!=tc.want {
			t.Errorf("isLegacyNumericTitle(%q): got %t, want %t",tc.title,got,tc.want)
		}
	}
}
func TestChatTitlePreservesAlreadyFormattedContact(t *testing.T) {
	c := database.KnownChat{WaChatID:"5511999999999@s.whatsapp.net",Name:"João (+5511999999999)",Kind:"private"}
	if got:=chatTitle(c);got!="João (+5511999999999)"{t.Fatalf("unexpected duplicated suffix: %s",got)}
}
