package whatsapp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"go.uber.org/zap"
)

// Priorities: 0 = stickers/photos, 1 = documents/voice/audio,
// 2 = videos/animations. All regular text, edits and deletions bypass
// the queue. Each queued job holds metadata, not media bytes.
type mediaJob struct {
	cfg     *state.Config
	message *events.Message
}

type mediaPlaceholder struct {
	chatID    int64
	messageID int64
}

var mediaQueues = [3]chan mediaJob{
	make(chan mediaJob, 128),
	make(chan mediaJob, 128),
	make(chan mediaJob, 64),
}
var mediaWorkerOnce sync.Once
var lastOverflowWarning atomic.Int64

var pendingMedia = struct {
	sync.Mutex
	items map[string]mediaPlaceholder
}{items: make(map[string]mediaPlaceholder)}

func mediaKey(v *events.Message) string {
	return v.Info.Chat.String() + "\x00" + v.Info.ID
}

// reservePendingMedia prevents repeated WhatsApp events from publishing
// duplicate pending notices and enqueuing the same download twice.
func reservePendingMedia(key string) bool {
	pendingMedia.Lock()
	defer pendingMedia.Unlock()
	if _, exists := pendingMedia.items[key]; exists {
		return false
	}
	pendingMedia.items[key] = mediaPlaceholder{}
	return true
}

func setPendingMedia(key string, info mediaPlaceholder) {
	pendingMedia.Lock()
	pendingMedia.items[key] = info
	pendingMedia.Unlock()
}

func mediaPlaceholderFor(v *events.Message) mediaPlaceholder {
	pendingMedia.Lock()
	defer pendingMedia.Unlock()
	return pendingMedia.items[mediaKey(v)]
}

func clearPendingMedia(key string) {
	pendingMedia.Lock()
	delete(pendingMedia.items, key)
	pendingMedia.Unlock()
}

func mediaPriority(message *waE2E.Message) int {
	if message == nil || message.GetProtocolMessage() != nil ||
		message.GetSecretEncryptedMessage() != nil {
		return -1
	}
	switch {
	case message.GetStickerMessage() != nil ||
		message.GetImageMessage() != nil:
		return 0
	case message.GetDocumentMessage() != nil ||
		message.GetAudioMessage() != nil:
		return 1
	case message.GetVideoMessage() != nil ||
		message.GetPtvMessage() != nil:
		return 2
	default:
		return -1
	}
}

func mediaDescription(message *waE2E.Message) (string, string) {
	if message == nil {
		return "Mídia", ""
	}
	var description, caption string
	switch {
	case message.GetStickerMessage() != nil:
		description = "Figurinha"
	case message.GetImageMessage() != nil:
		description, caption = "Foto", message.GetImageMessage().GetCaption()
	case message.GetDocumentMessage() != nil:
		description, caption = "Documento", message.GetDocumentMessage().GetCaption()
		if filename := message.GetDocumentMessage().GetFileName(); filename != "" {
			description += ": " + filename
		}
	case message.GetAudioMessage() != nil:
		if message.GetAudioMessage().GetPTT() {
			description = "Mensagem de voz"
		} else {
			description = "Áudio"
		}
	case message.GetVideoMessage() != nil:
		description, caption = "Vídeo", message.GetVideoMessage().GetCaption()
	case message.GetPtvMessage() != nil:
		description = "Vídeo circular"
	default:
		description = "Mídia"
	}
	if len([]rune(description)) > 100 {
		description = string([]rune(description)[:100]) + "…"
	}
	if len([]rune(caption)) > 350 {
		caption = string([]rune(caption)[:350]) + "…"
	}
	return description, caption
}

func pendingMediaText(v *events.Message) string {
	description, caption := mediaDescription(v.Message)
	lines := []string{description + " recebida — preparando arquivo para o Telegram."}
	if v.Info.IsGroup {
		if name := utils.WaGetContactName(v.Info.MessageSource.Sender); name != "" {
			lines = append([]string{name}, lines...)
		}
	}
	if caption != "" {
		lines = append(lines, caption)
	}
	return strings.Join(lines, "\n")
}

// showPendingMedia reserves the original chronological position. Once the
// upload finishes, Telegram can edit this very message into native media.
// Before completion, this text does not claim to be downloadable.
func showPendingMedia(cfg *state.Config, v *events.Message) mediaPlaceholder {
	if cfg == nil || !cfg.Telegram.SeparateChats || state.State.TelegramBot == nil ||
		(v.Info.IsFromMe && !cfg.WhatsApp.SendMyMessagesFromOtherDevices) {
		return mediaPlaceholder{}
	}
	id, found, err := database.TelegramGroupForWhatsApp(CanonicalChatJID(v.Info.Chat).String())
	if err != nil || !found {
		return mediaPlaceholder{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	msg, err := state.State.TelegramBot.SendMessageWithContext(
		ctx, id, pendingMediaText(v), &gotgbot.SendMessageOpts{
			DisableNotification: true,
		})
	if err != nil || msg == nil {
		if state.State.Logger != nil {
			state.State.Logger.Warn("unable to reserve Telegram media position", zap.Error(err))
		}
		return mediaPlaceholder{}
	}
	return mediaPlaceholder{chatID: id, messageID: msg.MessageId}
}

func updatePendingMediaText(placeholder mediaPlaceholder, text string) {
	if placeholder.messageID == 0 || state.State.TelegramBot == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	_, _, _ = state.State.TelegramBot.EditMessageTextWithContext(
		ctx, &gotgbot.EditMessageTextOpts{
			ChatId: placeholder.chatID, MessageId: placeholder.messageID, Text: text,
		})
}

func startMediaWorker() {
	mediaWorkerOnce.Do(func() {
		go func() {
			for {
				var job mediaJob
				select {
				case job = <-mediaQueues[0]:
				default:
					select {
					case job = <-mediaQueues[1]:
					default:
						select {
						case job = <-mediaQueues[0]:
						case job = <-mediaQueues[1]:
						case job = <-mediaQueues[2]:
						}
					}
				}
				if job.cfg == nil || job.message == nil {
					continue
				}
				key := mediaKey(job.message)
				func() {
					defer clearPendingMedia(key)
					handleMessageEvent(job.cfg, job.message)
				}()
			}
		}()
	})
}

// enqueueMedia reserves a Telegram message first, and only then queues the
// bytes-heavy work. A full queue leaves a clear status in the original chat.
func enqueueMedia(cfg *state.Config, v *events.Message) bool {
	if v == nil {
		return false
	}
	priority := mediaPriority(v.Message)
	if priority < 0 {
		return false
	}
	key := mediaKey(v)
	if !reservePendingMedia(key) {
		return true // Already registered and processing.
	}
	placeholder := showPendingMedia(cfg, v)
	setPendingMedia(key, placeholder)
	startMediaWorker()
	select {
	case mediaQueues[priority] <- mediaJob{cfg: cfg, message: v}:
		return true
	default:
		updatePendingMediaText(placeholder,
			"Mídia recebida, mas a fila está cheia. Consulte o arquivo original no WhatsApp.")
		clearPendingMedia(key)
		if state.State.Logger != nil {
			state.State.Logger.Warn("WhatsApp media queue is full",
				zap.Int("priority", priority), zap.String("chat", v.Info.Chat.String()))
		}
		now := time.Now().Unix()
		old := lastOverflowWarning.Load()
		if now-old >= 60 && lastOverflowWarning.CompareAndSwap(old, now) &&
			state.State.TelegramBot != nil && cfg != nil {
			_, _ = state.State.TelegramBot.SendMessage(cfg.Telegram.OwnerID,
				fmt.Sprintf("Fila de mídias cheia (prioridade %d). Textos continuam sendo encaminhados.", priority), nil)
		}
		return false
	}
}
