package telegram

import (
	"fmt"
	"html"
	"strings"
	"strconv"

	"watgbridge/database"
	"watgbridge/routeidentity"
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
		"Use <code>/conversas lista</code> ou <code>/conversas lista 2</code> para consultar todas as páginas, ou "+
		"<code>/conversas atualizar</code> para consultar novamente os grupos."

	if len(ctx.Args()) > 1 && strings.EqualFold(ctx.Args()[1], "lista") {
		page := 1
		if len(ctx.Args()) > 2 {
			n, parseErr := strconv.Atoi(ctx.Args()[2])
			if parseErr != nil || n < 1 {
				_, err := utils.TgReplyTextByContext(bot, ctx,
					"Informe uma página válida. Exemplo: /conversas lista 2", nil, false)
				return err
			}
			page = n
		}
		const pageSize = 20 // Telegram message size, NOT a limit on synced conversations.
		pages := int((stats.Total + pageSize - 1) / pageSize)
		if pages == 0 { pages = 1 }
		if page > pages {
			_, err := utils.TgReplyTextByContext(bot, ctx,
				fmt.Sprintf("Página %d não existe. Última página: %d.", page, pages), nil, false)
			return err
		}
		chats, err := database.ListKnownChatsPage(page, pageSize)
		if err != nil {
			return utils.TgReplyWithErrorByContext(bot, ctx, "Não consegui listar as conversas", err)
		}
		result += fmt.Sprintf("\n\n<b>Conversas — página %d de %d</b>\n", page, pages)
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
		if page < pages {
			result += fmt.Sprintf("\nPróxima página: <code>/conversas lista %d</code>", page+1)
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
	// Validate both sides BEFORE modifying a Telegram group's public
	// metadata. Refuse to touch groups already bound to a different chat.
	if tgID, linked, err := database.TelegramGroupForWhatsApp(waChatID); err!=nil {
		return utils.TgReplyWithErrorByContext(bot,ctx,"Falha na consulta de vínculos",err)
	} else if linked && tgID!=ctx.EffectiveChat.Id {
		_,replyErr:=utils.TgReplyTextByContext(bot,ctx,
			"Essa conversa já está vinculada a outro grupo. Não vou duplicá-la.",nil,false)
		return replyErr
	}
	if old, linked, err := database.WhatsAppChatForTelegramGroup(ctx.EffectiveChat.Id); err!=nil {
		return utils.TgReplyWithErrorByContext(bot,ctx,"Falha na consulta do grupo",err)
	} else if linked && old!=waChatID {
		_,replyErr:=utils.TgReplyTextByContext(bot,ctx,
			"Este grupo já pertence a outra conversa.",nil,false)
		return replyErr
	}
	tag, err := routeidentity.Encrypt(waChatID)
	if err != nil {
		return utils.TgReplyWithErrorByContext(bot,ctx,
			"É necessário configurar a chave segura WATG_BACKUP_KEY",err)
	}
	chat,err:=bot.GetChat(ctx.EffectiveChat.Id,nil)
	if err!=nil{return utils.TgReplyWithErrorByContext(bot,ctx,"Não consegui consultar a descrição",err)}
	if chat.Description!="" {
		existing,tagged,err:=routeidentity.Decrypt(chat.Description)
		if err!=nil || !tagged || existing!=waChatID {
			_,replyErr:=utils.TgReplyTextByContext(bot,ctx,
				"Grupo já tem uma descrição diferente. Não vou alterar ou vincular automaticamente.",nil,false)
			return replyErr
		}
	}
	if chat.Description!=tag {
		ok,err:=bot.SetChatDescription(ctx.EffectiveChat.Id,
			&gotgbot.SetChatDescriptionOpts{Description:tag})
		if err!=nil || !ok {
			return utils.TgReplyWithErrorByContext(bot,ctx,
				"É preciso tornar o bot administrador com permissão para editar o grupo",err)
		}
	}
	err = database.EnsureSeparateChat(waChatID, ctx.EffectiveChat.Id)
	if err != nil {
		return utils.TgReplyWithErrorByContext(bot, ctx,
			"Vínculo recusado para evitar conversas duplicadas", err)
	}
	_, err = utils.TgReplyTextByContext(bot, ctx,
		"Conversa vinculada e identificação criptografada guardada no Telegram. "+
			"Ela poderá ser recuperada sem duplicações após um reinício.", nil, false)
	return err
}
