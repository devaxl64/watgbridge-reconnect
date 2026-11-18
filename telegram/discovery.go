package telegram

import (
	"fmt"
	"crypto/sha256"
	"encoding/hex"
	"html"
	"strings"

	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/utils"
	"watgbridge/whatsapp"

	waTypes "go.mau.fi/whatsmeow/types"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// ConversationsCommandHandler provides owner-only visibility into WhatsApp
// chat discovery. It never sends historical messages or attachments.
func ConversationsCommandHandler(bot *gotgbot.Bot, ctx *ext.Context) error {
	if ctx.EffectiveSender == nil || ctx.EffectiveSender.User == nil ||
		ctx.EffectiveSender.User.Id != state.State.Config.Telegram.OwnerID ||
		ctx.EffectiveChat == nil ||
		ctx.EffectiveChat.Id != state.State.Config.Telegram.OwnerID {
		// Listing chats is more sensitive than normal bridge commands;
		// it is only allowed in the owner's private conversation.
		return nil
	}

	if len(ctx.Args()) > 1 && ctx.Args()[1] == "atualizar" {
		if client := state.State.WhatsAppClient; client == nil || !client.IsLoggedIn() {
			_, err := utils.TgReplyTextByContext(bot, ctx,
				"O WhatsApp não está conectado. Use /status.", nil, false)
			return err
		}
		whatsapp.StartJoinedGroupDiscovery()
		_, err := utils.TgReplyTextByContext(bot, ctx,
			"Solicitei a atualização da lista de grupos. Aguarde alguns instantes e use /conversas.", nil, false)
		return err
	}

	stats, err := database.GetKnownChatStats()
	if err != nil {
		return utils.TgReplyWithErrorByContext(bot, ctx, "Não consegui consultar as conversas", err)
	}
	pending := stats.Total - stats.Linked
	if pending < 0 {
		pending = 0
	}
	result := fmt.Sprintf(
		"<b>Conversas identificadas no WhatsApp</b>\n"+
			"Individuais: <b>%d</b>\nGrupos: <b>%d</b>\n"+
			"Total: <b>%d</b>\nCom grupo Telegram vinculado: <b>%d</b>\n"+
			"Aguardando vinculação: <b>%d</b>\n\n",
		stats.Private, stats.Groups, stats.Total, stats.Linked, pending,
	)
	result += "O catálogo contém apenas identificadores e nomes, sem histórico de mensagens. "+
		"Na primeira vinculação, o WhatsApp pode enviar mais conversas que numa simples reconexão.\n"+
		"Use <code>/conversas lista</code> para listar até 20 conversas ou "+
		"<code>/conversas atualizar</code> para consultar novamente os grupos."

	if len(ctx.Args()) > 1 && strings.EqualFold(ctx.Args()[1], "lista") {
		chats, err := database.ListKnownChats(20)
		if err != nil {
			return utils.TgReplyWithErrorByContext(bot, ctx, "Não consegui listar as conversas", err)
		}
		result += "\n\n<b>Conversas mais recentes</b>\n"
		for _, chat := range chats {
			label := strings.TrimSpace(chat.Name)
			if label == "" {
				label = chat.WaChatID
				// Do not display the WhatsApp server suffix in the title.
				if idx := strings.IndexByte(label, '@'); idx >= 0 {
					label = label[:idx]
				}
				if strings.HasPrefix(label, "55") && len(label) > 11 {
					label = label[2:]
				}
			}
			if len([]rune(label)) > 60 {
				label = string([]rune(label)[:60])
			}
			typeName := "Contato"
			if chat.Kind == "group" {
				typeName = "Grupo"
			}
			result += fmt.Sprintf("• %s: %s\n", typeName, html.EscapeString(label))
		}
	}
	_, err = utils.TgReplyTextByContext(bot, ctx, result, nil, false)
	return err
}

// LinkDedicatedGroupHandler is a manual/test fallback while automatic group
// provisioning via the owner's MTProto session is under development. It never
// creates groups, never links a Telegram group to two WhatsApp JIDs, and never
// exposes a full contact number in the Telegram group description.
func LinkDedicatedGroupHandler(bot *gotgbot.Bot, ctx *ext.Context) error {
	if ctx.EffectiveSender == nil || ctx.EffectiveSender.User == nil ||
		ctx.EffectiveSender.User.Id != state.State.Config.Telegram.OwnerID ||
		ctx.EffectiveChat == nil {
		return nil
	}
	if ctx.EffectiveChat.Type != "supergroup" && ctx.EffectiveChat.Type != "group" {
		_, err := utils.TgReplyTextByContext(bot, ctx,
			"Use este comando dentro do grupo do Telegram que será vinculado.", nil, false)
		return err
	}
	if ctx.EffectiveChat.Id == state.State.Config.Telegram.TargetChatID {
		_, err := utils.TgReplyTextByContext(bot, ctx,
			"Este é o grupo antigo com tópicos. Crie um grupo separado para este teste.", nil, false)
		return err
	}
	args := ctx.Args()
	if len(args) != 2 {
		_, err := utils.TgReplyTextByContext(bot, ctx,
			"Uso: <code>/vincular 5511999999999</code> ou <code>/vincular 12345@g.us</code>.", nil, false)
		return err
	}
	raw := args[1]
	if strings.HasPrefix(raw, "+") { raw = raw[1:] }
	if !strings.Contains(raw, "@") {
		raw += "@s.whatsapp.net"
	}
	jid, err := waTypes.ParseJID(raw)
	if err != nil || jid.User == "" ||
		(jid.Server != "s.whatsapp.net" && jid.Server != "g.us" && jid.Server != "lid") {
		_, replyErr := utils.TgReplyTextByContext(bot, ctx, "Identificador WhatsApp inválido.", nil, false)
		return replyErr
	}
	waChatID := jid.ToNonAD().String()
	err = database.EnsureSeparateChat(waChatID, ctx.EffectiveChat.Id)
	if err != nil {
		return utils.TgReplyWithErrorByContext(bot, ctx,
			"Vínculo recusado para evitar conversas duplicadas", err)
	}

	// A non-sensitive fingerprint in the group description will later let
	// the user MTProto importer find the same group after a local DB loss.
	sum := sha256.Sum256([]byte(waChatID))
	tag := "WATGBRIDGE-ID:" + hex.EncodeToString(sum[:12])
	remoteSaved := false
	if chat, getErr := bot.GetChat(ctx.EffectiveChat.Id, nil); getErr == nil &&
		(chat.Description == "" || chat.Description == tag) {
		remoteSaved, _ = bot.SetChatDescription(ctx.EffectiveChat.Id,
			&gotgbot.SetChatDescriptionOpts{Description: tag})
	}
	msg := "Conversa vinculada. Mensagens dessa conversa poderão usar este grupo do Telegram."
	if !remoteSaved {
		msg += "\nA identificação remota não foi atualizada; o bot precisa ser administrador com permissão para editar o grupo."
	}
	_, err = utils.TgReplyTextByContext(bot, ctx, msg, nil, false)
	return err
}
