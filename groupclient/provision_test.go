package groupclient

import (
	"testing"

	"watgbridge/database"
)

func TestChatTitlesWithoutEmojis(t *testing.T){
	tests:=[]struct{in database.KnownChat;want string}{
		{database.KnownChat{WaChatID:"5511999999999@s.whatsapp.net",Name:"João",Kind:"private"},"João (11999999999)"},
		{database.KnownChat{WaChatID:"123@g.us",Name:"Amigos",Kind:"group"},"Amigos"},
		{database.KnownChat{WaChatID:"5511888888888@s.whatsapp.net",Kind:"private"},"11888888888"},
	}
	for _,c:=range tests{
		if got:=chatTitle(c.in);got!=c.want {t.Errorf("title: %q, want %q",got,c.want)}
	}
}
func TestSupergroupID(t *testing.T) {
	if got:=SupergroupID(3987753765);got!=-1003987753765{t.Errorf("wrong supergroup ID: %d",got)}
}
