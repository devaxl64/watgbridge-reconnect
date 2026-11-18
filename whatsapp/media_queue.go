package whatsapp

import (
	"sync"
	"sync/atomic"
	"time"

	"watgbridge/state"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"go.uber.org/zap"
)

// Media operations are intentionally processed behind chat discovery and text
// messages. A long video upload must not block a newly arrived text message.
//
// Priority 0: documents/voice/audio.
// Priority 1: photos/stickers.
// Priority 2: videos/GIFs.
// Each level has bounded metadata-only buffers, not decoded file contents.
type mediaJob struct {
	cfg *state.Config
	message *events.Message
}

var mediaQueues = [3]chan mediaJob{
	make(chan mediaJob, 128),
	make(chan mediaJob, 128),
	make(chan mediaJob, 64),
}
var mediaWorkerOnce sync.Once
var lastOverflowWarning atomic.Int64

func mediaPriority(message *waE2E.Message) int {
	if message == nil || message.GetProtocolMessage() != nil ||
		message.GetSecretEncryptedMessage() != nil {
		return -1
	}
	switch {
	case message.GetDocumentMessage() != nil ||
		message.GetAudioMessage() != nil:
		return 0
	case message.GetImageMessage() != nil ||
		message.GetStickerMessage() != nil:
		return 1
	case message.GetVideoMessage() != nil ||
		message.GetPtvMessage() != nil:
		return 2
	default:
		// Interactive messages, edits and service messages are processed
		// immediately so their contents and changes aren't delayed.
		return -1
	}
}

func startMediaWorker() {
	mediaWorkerOnce.Do(func() {
		go func() {
			// A single worker bounds CPU, temporary files and RAM. Separate
			// buffered queues let light media jump ahead of queued videos.
			for {
				var job mediaJob
				var ok bool
				select {
				case job, ok = <-mediaQueues[0]:
				default:
					select {
					case job, ok = <-mediaQueues[1]:
					default:
						select {
						case job, ok = <-mediaQueues[0]:
						case job, ok = <-mediaQueues[1]:
						case job, ok = <-mediaQueues[2]:
						}
					}
				}
				if ok && job.cfg != nil && job.message != nil {
					handleMessageEvent(job.cfg, job.message)
				}
			}
		}()
	})
}

// enqueueMedia prioritizes delivery without buffering the media itself.
// Returns false if the bounded queue is full; the caller can report that
// WhatsApp still has the original file, rather than silently discarding it.
func enqueueMedia(cfg *state.Config, v *events.Message) bool {
	if v == nil { return false }
	p := mediaPriority(v.Message)
	if p < 0 { return false }
	startMediaWorker()
	select {
	case mediaQueues[p] <- mediaJob{cfg:cfg,message:v}:
		return true
	default:
		if state.State.Logger != nil {
			state.State.Logger.Warn("WhatsApp media queue is full",
				zap.Int("priority",p),zap.String("chat",v.Info.Chat.String()))
		}
		now:=time.Now().Unix()
		old:=lastOverflowWarning.Load()
		if now-old>=60 && lastOverflowWarning.CompareAndSwap(old,now) &&
			state.State.TelegramBot != nil && cfg != nil {
			_,_ = state.State.TelegramBot.SendMessage(cfg.Telegram.OwnerID,
				"A fila temporária de mídias está cheia. As mensagens de texto continuam chegando; confira mídias pendentes no WhatsApp.",nil)
		}
		return false
	}
}
