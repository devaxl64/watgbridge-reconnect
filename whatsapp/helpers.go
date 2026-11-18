package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"io"
	"syscall"
	"time"

	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/transfer"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.uber.org/zap"
)

// bridgeContext holds the common state needed when bridging a single
// WhatsApp message to Telegram. It is created once per incoming message
// and passed to the individual media-type handlers.
type bridgeContext struct {
	cfg          *state.Config
	logger       *zap.Logger
	tgBot        *gotgbot.Bot
	waClient     *whatsmeow.Client
	bridgedText  string
	replyToMsgId int64
	tgChatID     int64
	threadId     int64
	placeholderID int64
	msgId        string
	senderStr    string
	chatStr      string
	replyMarkup  gotgbot.InlineKeyboardMarkup
}

// savePair persists the WA↔TG message-ID mapping if the Telegram message
// was sent successfully.
func (bc *bridgeContext) savePair(sentMsg *gotgbot.Message) {
	if sentMsg != nil && sentMsg.MessageId != 0 {
		database.MsgIdAddNewPair(
			bc.msgId, bc.senderStr, bc.chatStr,
			bc.tgChatID,
			sentMsg.MessageId, sentMsg.MessageThreadId,
		)
		if bc.placeholderID != 0 && bc.placeholderID != sentMsg.MessageId {
			// Native stickers can't replace text via editMessageMedia.
			_, _ = bc.tgBot.DeleteMessage(bc.tgChatID, bc.placeholderID, nil)
			bc.placeholderID = 0
		}
		// Retain the in-flight reservation until the worker finishes, but
		// mark the notice as delivered so it is not turned into an error.
		setPendingMedia(bc.chatStr+"\x00"+bc.msgId, mediaPlaceholder{})
	}
}

// editPendingMedia uses Telegram's editMessageMedia API to replace the
// initial waiting notice with the actual photo, video, document or audio.
// This keeps the original chronological position of incoming WhatsApp media,
// even when transfers were deferred behind more urgent messages.
func (bc *bridgeContext) editPendingMedia(media gotgbot.InputMedia) bool {
	if bc.placeholderID == 0 || bc.tgBot == nil || media == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	edited, ok, err := bc.tgBot.EditMessageMediaWithContext(ctx, media,
		&gotgbot.EditMessageMediaOpts{
			ChatId: bc.tgChatID, MessageId: bc.placeholderID,
		})
	if err != nil || !ok {
		if bc.logger != nil {
			bc.logger.Warn("Telegram could not replace media notice in-place",
				zap.Error(err), zap.Int64("telegram_chat", bc.tgChatID))
		}
		return false
	}
	originalID := bc.placeholderID
	bc.placeholderID = 0
	if edited == nil {
		edited = &gotgbot.Message{MessageId: originalID}
	}
	bc.savePair(edited)
	return true
}

// sendFallbackText sends a text-only message (header + extra info) to
// Telegram and saves the pair. Used when media cannot be sent.
func (bc *bridgeContext) sendFallbackText(extraText string) {
	if bc.placeholderID != 0 {
		// Preserve an immediately registered media record even if WhatsApp
		// download fails, the file is oversized, or Telegram rejects it.
		// This notice is not a downloadable file and says so explicitly.
		text := "Mídia recebida, mas o arquivo não pôde ser enviado agora. " +
			"Ele continua disponível no WhatsApp, se ainda não tiver expirado."
		_, ok, err := bc.tgBot.EditMessageText(&gotgbot.EditMessageTextOpts{
			ChatId: bc.tgChatID, MessageId: bc.placeholderID, Text: text,
		})
		if err == nil && ok {
			id := bc.placeholderID
			bc.placeholderID = 0
			bc.savePair(&gotgbot.Message{MessageId: id})
			return
		}
	}
	sentMsg, _ := bc.tgBot.SendMessage(
		bc.tgChatID,
		bc.bridgedText+extraText,
		&gotgbot.SendMessageOpts{
			ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
			MessageThreadId: bc.threadId,
		},
	)
	bc.savePair(sentMsg)
}

// mediaTransferSlot keeps at most one WhatsApp media byte buffer alive while
// it is being uploaded to Telegram. On 256 MB RAM, downloading several 50 MB
// videos simultaneously would crash the process.
var mediaTransferSlot = make(chan struct{}, 1)
var mediaTransferBudget transfer.Limiter
// Only sticker conversions still require bytes in memory; cap them hard.
const maxMediaInRAM int64 = 8 * 1024 * 1024

// downloadMedia returns a release callback that MUST run after the Telegram
// upload (not immediately after WhatsApp download) so byte buffers cannot
// overlap. This limit is independent of the planned 600MB disk quota.
func (bc *bridgeContext) downloadMedia(message whatsmeow.DownloadableMessage, declaredBytes uint64) ([]byte, func(), error) {
	size := int64(declaredBytes)
	if size <= 0 || size > maxMediaInRAM {
		return nil,nil,errors.New("mídia acima de 48 MB ou sem tamanho informado; envio seguro indisponível")
	}
	select {
	case mediaTransferSlot <- struct{}{}:
	case <-time.After(2 * time.Minute):
		return nil,nil,errors.New("limite de transferência ocupado; tente novamente mais tarde")
	}
	releaseSpace,err:=mediaTransferBudget.Reserve(size)
	if err!=nil {
		<-mediaTransferSlot
		return nil,nil,err
	}
	release:=func(){releaseSpace();<-mediaTransferSlot}
	ctx,cancel:=context.WithTimeout(context.Background(),3*time.Minute)
	defer cancel()
	data,err:=bc.waClient.Download(ctx,message)
	if err!=nil {
		release()
		return nil,nil,err
	}
	if len(data)>int(maxMediaInRAM){
		release()
		return nil,nil,errors.New("mídia ultrapassou o limite seguro de memória")
	}
	return data,release,nil
}

// downloadMediaToTemp streams directly from authenticated WhatsApp media
// storage into an ephemeral file. The media never exists as a full []byte in
// the process, so Telegram can upload it with minimal additional RAM.
// The caller MUST defer the returned cleanup function until the Telegram
// upload finishes. The file is deleted immediately afterwards.
func (bc *bridgeContext) downloadMediaToTemp(message whatsmeow.DownloadableMessage, declaredBytes uint64) (*os.File, func(), error) {
	const maxTempFile = int64(600 * 1024 * 1024)
	if declaredBytes == 0 || declaredBytes > uint64(maxTempFile) {
		return nil, nil, errors.New("mídia sem tamanho conhecido ou maior que 600 MB")
	}
	size := int64(declaredBytes)
	// Only one heavy media download/upload at a time on 256 MB of RAM.
	select {
	case mediaTransferSlot <- struct{}{}:
	case <-time.After(2 * time.Minute):
		return nil,nil,errors.New("fila de downloads ocupada; tente novamente")
	}
	releaseSpace,err:=mediaTransferBudget.Reserve(size)
	if err!=nil { <-mediaTransferSlot; return nil,nil,err }

	// The advertised 600 MB budget is an upper limit, not a guarantee that
	// the ephemeral disk really has 600 MB free. Protect at least 100 MiB
	// of space for SQLite, login state and the running process.
	var disk syscall.Statfs_t
	if err:=syscall.Statfs(os.TempDir(),&disk);err!=nil {
		releaseSpace(); <-mediaTransferSlot
		return nil,nil,err
	}
	available:=uint64(disk.Bavail)*uint64(disk.Bsize)
	if available < uint64(size)+100*1024*1024 {
		releaseSpace(); <-mediaTransferSlot
		return nil,nil,errors.New("sem espaço temporário suficiente para mídia e margem de segurança")
	}

	file,err:=os.CreateTemp("", "watg-media-*.tmp")
	if err!=nil {releaseSpace();<-mediaTransferSlot;return nil,nil,err}
	cleanup:=func(){
		_ = file.Close()
		_ = os.Remove(file.Name())
		releaseSpace()
		<-mediaTransferSlot
	}
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Minute)
	defer cancel()
	// DownloadToFile uses WhatsApp's authenticated, encrypted-media download.
	// Unlike Download, it does not assemble a full media []byte in RAM.
	if err:=bc.waClient.DownloadToFile(ctx,message,file);err!=nil {
		cleanup();return nil,nil,err
	}
	info,err:=file.Stat()
	if err!=nil {cleanup();return nil,nil,err}
	if info.Size()<=0||info.Size()>maxTempFile||info.Size()>size+1024*1024 {
		cleanup();return nil,nil,errors.New("arquivo excedeu o espaço reservado")
	}
	if _,err:=file.Seek(0,io.SeekStart);err!=nil {
		cleanup();return nil,nil,err
	}
	return file,cleanup,nil
}

// addCaption appends a caption (truncated at 1020 chars if needed) to
// the bridged text.
func addCaption(bridgedText *string, caption string) {
	if caption == "" {
		return
	}
	if len(caption) > 1020 {
		*bridgedText += html.EscapeString(utils.SubString(caption, 0, 1020)) + "..."
	} else {
		*bridgedText += html.EscapeString(caption)
	}
}

// getContextInfo extracts the ContextInfo from any WhatsApp message type.
// Protobuf nil-safe getters make chained calls safe even when the
// sub-message is nil.
func getContextInfo(msg *waE2E.Message) *waE2E.ContextInfo {
	if ci := msg.GetExtendedTextMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetImageMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetVideoMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetPtvMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetAudioMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetDocumentMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetStickerMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetContactMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetContactsArrayMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetLocationMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetLiveLocationMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetPollCreationMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetPollCreationMessageV2().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetPollCreationMessageV3().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetButtonsMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetTemplateMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetInteractiveMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetListMessage().GetContextInfo(); ci != nil {
		return ci
	}
	return nil
}

// resolveThreadId determines the correct Telegram thread (topic) for a
// WhatsApp message based on chat type: status, broadcast, group, or private.
func resolveThreadId(info waTypes.MessageInfo, cfg *state.Config, tgBot *gotgbot.Bot) (int64, error) {
	chatStr := info.Chat.String()

	if chatStr == "status@broadcast" {
		return utils.TgGetOrMakeThreadFromWa_String(
			"status@broadcast", cfg.Telegram.TargetChatID, "Status",
		)
	}

	if info.IsIncomingBroadcast() {
		if info.MessageSource.AddressingMode == waTypes.AddressingModePN || info.MessageSource.SenderAlt.IsEmpty() {
			jid := info.MessageSource.Sender.ToNonAD()
			return utils.TgGetOrMakeThreadFromWa(jid, cfg.Telegram.TargetChatID, utils.WaGetContactName(jid))
		}
		jid := info.MessageSource.SenderAlt.ToNonAD()
		return utils.TgGetOrMakeThreadFromWa(jid, cfg.Telegram.TargetChatID, utils.WaGetContactName(jid))
	}

	if info.IsGroup {
		return utils.TgGetOrMakeThreadFromWa(
			info.Chat, cfg.Telegram.TargetChatID, utils.WaGetGroupName(info.Chat),
		)
	}

	targetJID := info.Chat.ToNonAD()
	return utils.TgGetOrMakeThreadFromWa(
		targetJID, cfg.Telegram.TargetChatID, utils.WaGetContactName(targetJID),
	)
}

// buildBridgedHeader builds the sender/group/timestamp header that
// precedes every bridged message.
func buildBridgedHeader(info waTypes.MessageInfo, cfg *state.Config, isEdited bool) string {
	var text string

	if cfg.WhatsApp.SkipChatDetails {
		if info.IsIncomingBroadcast() {
			text += "👥: <b>(Broadcast)</b>\n"
		} else if info.IsFromMe {
			text += "🧑: <b>You [other device]</b>\n"
		} else if info.IsGroup {
			text += fmt.Sprintf("🧑: <b>%s</b>\n",
				html.EscapeString(utils.WaGetContactName(info.MessageSource.Sender)))
		}
	} else {
		if info.IsFromMe {
			text += "🧑: <b>You [other device]</b>\n"
		} else {
			text += fmt.Sprintf("🧑: <b>%s</b>\n",
				html.EscapeString(utils.WaGetContactName(info.MessageSource.Sender)))
		}
		if info.IsIncomingBroadcast() {
			text += "👥: <b>(Broadcast)</b>\n"
		} else if info.IsGroup {
			text += fmt.Sprintf("👥: <b>%s</b>\n",
				html.EscapeString(utils.WaGetGroupName(info.Chat)))
		} else {
			text += "👥: <b>(PVT)</b>\n"
		}
	}

	if isEdited {
		text += "<i>Edited</i>\n"
	}

	if time.Since(info.Timestamp).Seconds() > 60 {
		text += fmt.Sprintf("🕛: <b>%s</b>\n",
			html.EscapeString(info.Timestamp.In(state.State.LocalLocation).Format(cfg.TimeFormat)))
	}

	return text
}
