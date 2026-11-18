package whatsapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"watgbridge/database"
	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.uber.org/zap"
)

const maxAvatarBytes = 1024 * 1024
const avatarCheckCooldown = 90 * time.Second

// avatarScanner coalesces a burst of WhatsApp messages from the same contact
// without storing image bytes or spawning hundreds of HTTP requests.
var avatarScanner = struct {
	sync.Mutex
	pending map[string]bool
	lastCheck map[string]time.Time
}{pending: map[string]bool{}, lastCheck: map[string]time.Time{}}

var avatarHTTPClient = &http.Client{Timeout: 15 * time.Second}

// QueueProfileRefresh checks the WhatsApp contact/group profile photo whenever
// a message arrives, bounded to one check per chat per 90 seconds. Only a small
// thumbnail is downloaded, and never stored permanently on Northflank.
func QueueProfileRefresh(jid waTypes.JID) {
	if !state.State.Config.Telegram.SeparateChats ||
		state.State.WhatsAppClient == nil ||
		!state.State.WhatsAppClient.IsLoggedIn() {
		return
	}
	jid = CanonicalChatJID(jid)
	key := jid.String()
	avatarScanner.Lock()
	last := avatarScanner.lastCheck[key]
	if avatarScanner.pending[key] || time.Since(last) < avatarCheckCooldown {
		avatarScanner.Unlock()
		return
	}
	avatarScanner.pending[key] = true
	avatarScanner.lastCheck[key] = time.Now()
	avatarScanner.Unlock()

	go func() {
		defer func() {
			avatarScanner.Lock()
			delete(avatarScanner.pending, key)
			avatarScanner.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if err := UpdateProfilePictureForChat(ctx, jid); err != nil && state.State.Logger != nil {
			state.State.Logger.Debug("could not refresh Telegram chat avatar",
				zap.String("whatsapp_chat", key), zap.Error(err))
		}
	}()
}

// DownloadAvatarPreview securely fetches only WhatsApp's small preview picture.
// It is not a generic URL fetcher and its caller must use a trusted WhatsApp
// picture URL returned by GetProfilePictureInfo.
func DownloadAvatarPreview(ctx context.Context, imageURL string) ([]byte, error) {
	u, err := url.Parse(imageURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, errors.New("invalid HTTPS avatar URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil { return nil, err }
	res, err := avatarHTTPClient.Do(req)
	if err != nil { return nil, err }
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK { return nil, fmt.Errorf("avatar HTTP %d", res.StatusCode) }
	if res.ContentLength > maxAvatarBytes { return nil, errors.New("avatar exceeds 1MB") }
	content, err := io.ReadAll(io.LimitReader(res.Body, maxAvatarBytes+1))
	if err != nil { return nil, err }
	if len(content) == 0 || len(content) > maxAvatarBytes { return nil, errors.New("avatar is empty or exceeds 1MB") }
	format := http.DetectContentType(content)
	if format != "image/jpeg" && format != "image/png" {
		return nil, errors.New("WhatsApp avatar preview is not JPEG/PNG")
	}
	return content, nil
}

func UpdateProfilePictureForChat(ctx context.Context, jid waTypes.JID) error {
	if !state.State.Config.Telegram.SeparateChats { return nil }
	jid = CanonicalChatJID(jid)
	chatID := jid.String()
	tgID, linked, err := database.TelegramGroupForWhatsApp(chatID)
	if err != nil || !linked { return err }
	wa := state.State.WhatsAppClient
	bot := state.State.TelegramBot
	if wa == nil || bot == nil || !wa.IsLoggedIn() { return errors.New("WhatsApp or Telegram is not connected") }

	oldID, _, err := database.KnownAvatarVersion(chatID)
	if err != nil { return err }
	picture, err := wa.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{
		Preview: true,
		ExistingID: oldID,
	})
	if err != nil { return err }
	// ExistingID lets WhatsApp return nil when the picture is unchanged.
	if picture == nil || picture.URL == "" || picture.ID == "" { return nil }
	if picture.ID == oldID { return nil }
	preview, err := DownloadAvatarPreview(ctx, picture.URL)
	if err != nil { return err }
	_, err = bot.SetChatPhotoWithContext(ctx, tgID,
		&gotgbot.FileReader{Data: bytes.NewReader(preview)},
		&gotgbot.SetChatPhotoOpts{})
	if err != nil { return fmt.Errorf("Telegram bot needs chat-photo admin permission: %w", err) }
	return database.SetKnownAvatarVersion(chatID, picture.ID)
}
