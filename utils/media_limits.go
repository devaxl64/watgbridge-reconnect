package utils

import (
	"fmt"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// MaxForwardMediaSize is the deliberately conservative per-file limit
// requested for Telegram -> WhatsApp transfers, even with a local Bot API.
const MaxForwardMediaSize int64 = 50 * 1024 * 1024

// TgOutgoingMediaSize reads the size provided by Telegram without fetching
// any bytes. Zero means that Telegram did not report a usable file size.
func TgOutgoingMediaSize(msg *gotgbot.Message) int64 {
	if msg == nil { return 0 }
	switch {
	case msg.Document != nil:
		return msg.Document.FileSize
	case msg.Video != nil:
		return msg.Video.FileSize
	case msg.VideoNote != nil:
		return msg.VideoNote.FileSize
	case msg.Animation != nil:
		return msg.Animation.FileSize
	case msg.Audio != nil:
		return msg.Audio.FileSize
	case msg.Voice != nil:
		return msg.Voice.FileSize
	case msg.Sticker != nil:
		return msg.Sticker.FileSize
	case len(msg.Photo) > 0:
		var max int64
		for _, photo := range msg.Photo {
			if photo.FileSize > max { max = photo.FileSize }
		}
		return max
	default:
		return 0
	}
}

// TgOutgoingMediaLimitNotice is a local Telegram-only response. It must be
// evaluated before attempting to forward content to WhatsApp.
// The hosted Bot API can upload 50 MiB but DOWNLOAD only 20 MiB.
func TgOutgoingMediaLimitNotice(msg *gotgbot.Message, selfHostedAPI bool) string {
	size := TgOutgoingMediaSize(msg)
	if size <= 0 { return "" }
	actualSize := strings.ReplaceAll(fmt.Sprintf("%.1f", float64(size)/(1024*1024)), ".", ",")
	if size > MaxForwardMediaSize {
		return fmt.Sprintf("Não foi possível baixar o arquivo (%s MB). Limite configurado: 50 MB.", actualSize)
	}
	if !selfHostedAPI && size > DownloadSizeLimit {
		return fmt.Sprintf("Não foi possível baixar o arquivo (%s MB). Limite do bot: 20 MB.", actualSize)
	}
	return ""
}
