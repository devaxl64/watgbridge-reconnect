package utils

import (
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

func TestTgOutgoingMediaLimitNoticeAllTypes(t *testing.T) {
	big := int64(51 * 1024 * 1024)
	uploads := []struct {
		name string
		msg *gotgbot.Message
	}{
		{"document", &gotgbot.Message{Document: &gotgbot.Document{FileSize: big, FileName: "report.pdf"}}},
		{"video", &gotgbot.Message{Video: &gotgbot.Video{FileSize: big}}},
		{"video note", &gotgbot.Message{VideoNote: &gotgbot.VideoNote{FileSize: big}}},
		{"animation", &gotgbot.Message{Animation: &gotgbot.Animation{FileSize: big}}},
		{"audio", &gotgbot.Message{Audio: &gotgbot.Audio{FileSize: big}}},
		{"voice", &gotgbot.Message{Voice: &gotgbot.Voice{FileSize: big}}},
		{"sticker", &gotgbot.Message{Sticker: &gotgbot.Sticker{FileSize: big}}},
		{"photo", &gotgbot.Message{Photo: []gotgbot.PhotoSize{{FileSize: 1}, {FileSize: big}}}},
	}
	for _, tc := range uploads {
		t.Run(tc.name, func(t *testing.T) {
			if size := TgOutgoingMediaSize(tc.msg); size != big {
				t.Fatalf("size = %d, want %d", size, big)
			}
			for _, selfHosted := range []bool{false, true} {
				msg := TgOutgoingMediaLimitNotice(tc.msg, selfHosted)
				if !strings.Contains(msg, "Não foi possível baixar o arquivo") ||
					!strings.Contains(msg, "(51,0 MB)") ||
					!strings.Contains(msg, "Limite configurado: 50 MB.") {
					t.Fatalf("missing local 50 MB rejection: %q", msg)
				}
			}
		})
	}
}

func TestTgOutgoingMediaLimitNoticeHostedDownloadLimit(t *testing.T) {
	msg := &gotgbot.Message{Document: &gotgbot.Document{FileSize: 25 * 1024 * 1024, FileName: "video.mp4", MimeType: "video/mp4"}}
	notice := TgOutgoingMediaLimitNotice(msg, false)
	if !strings.Contains(notice, "Limite do bot: 20 MB.") ||
		!strings.Contains(notice, "(25,0 MB)") ||
		!strings.Contains(notice, "Limite configurado: 50 MB.") {
		t.Fatalf("did not explain hosted download limit: %q", notice)
	}
	if got := TgOutgoingMediaLimitNotice(msg, true); got != "" {
		t.Fatalf("local Bot API should allow a 25 MB transfer, got %q", got)
	}
}

func TestTgOutgoingMediaLimitBoundariesAndText(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg *gotgbot.Message
		selfHosted bool
		wantNotice bool
	}{
		{"text", &gotgbot.Message{Text: "Normal"}, false, false},
		{"empty", &gotgbot.Message{}, false, false},
		{"nil", nil, false, false},
		{"unknown size", &gotgbot.Message{Document: &gotgbot.Document{FileSize: 0}}, false, false},
		{"hosted 20", &gotgbot.Message{Video: &gotgbot.Video{FileSize: DownloadSizeLimit}}, false, false},
		{"hosted 20 plus 1", &gotgbot.Message{Video: &gotgbot.Video{FileSize: DownloadSizeLimit + 1}}, false, true},
		{"local 50", &gotgbot.Message{Document: &gotgbot.Document{FileSize: MaxForwardMediaSize}}, true, false},
		{"local 50 plus 1", &gotgbot.Message{Document: &gotgbot.Document{FileSize: MaxForwardMediaSize + 1}}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := TgOutgoingMediaLimitNotice(tc.msg, tc.selfHosted)
			if (got != "") != tc.wantNotice {
				t.Fatalf("got notice %q, wantNotice %t", got, tc.wantNotice)
			}
		})
	}
}
