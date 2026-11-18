package whatsapp

import (
	"testing"

	waTypes "go.mau.fi/whatsmeow/types"
)

func TestVideoFileNameDoesNotPanicOnMalformedMIME(t *testing.T) {
	for _, tc := range []struct{mime, want string}{
		{"", "video.mp4"},
		{"broken", "video.mp4"},
		{"video/mp4", "video.mp4"},
		{"video/webm", "video.webm"},
		{"video/3gpp", "video.3gp"},
		{"VIDEO/QUICKTIME; charset=utf-8", "video.mov"},
		{"application/octet-stream", "video.mp4"},
	} {
		if got := videoFileName(tc.mime); got != tc.want {
			t.Errorf("videoFileName(%q) = %q, want %q", tc.mime, got, tc.want)
		}
	}
}

func TestGroupSenderLabelNeverTreatsLIDAsPhone(t *testing.T) {
	phone := waTypes.JID{Server: "s.whatsapp.net", User: "5511987654321"}
	lid := waTypes.JID{Server: "lid", User: "234567890123"}
	empty := waTypes.JID{}
	if got := groupSenderLabel("Ana", lid, empty); got != "Ana" {
		t.Fatalf("LID must not become a phone number: %q", got)
	}
	if got := groupSenderLabel("Ana", lid, phone); got != "Ana (11987654321)" {
		t.Fatalf("alternative PN should be used: %q", got)
	}
	if got := groupSenderLabel("Ana", phone, empty); got != "Ana (11987654321)" {
		t.Fatalf("direct PN should be used: %q", got)
	}
	if got := groupSenderLabel("11987654321", phone, empty); got != "11987654321" {
		t.Fatalf("must not duplicate a numeric display name: %q", got)
	}
}
