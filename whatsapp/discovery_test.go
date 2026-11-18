package whatsapp

import (
	"testing"
)

func TestCanonicalChatID(t *testing.T) {
	cases := []struct{
		in string
		wantID string
		kind string
		ok bool
	}{
		{"5511999999999@s.whatsapp.net", "5511999999999@s.whatsapp.net", "private", true},
		{"12345@g.us", "12345@g.us", "group", true},
		{"12345:13@s.whatsapp.net", "12345@s.whatsapp.net", "private", true},
		{"20222@lid", "20222@lid", "private", true},
		{"status@broadcast", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases{
		got,kind,ok := canonicalChatID(tc.in)
		if got!=tc.wantID || kind!=tc.kind || ok!=tc.ok {
			t.Errorf("canonicalChatID(%q) = %q, %q, %t",tc.in,got,kind,ok)
		}
	}
}
