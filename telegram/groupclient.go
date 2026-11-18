package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"watgbridge/groupclient"
	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

func isPrivateOwner(ctx *ext.Context) bool {
	return ctx != nil && ctx.EffectiveSender != nil &&
		ctx.EffectiveSender.User != nil &&
		ctx.EffectiveSender.User.Id == state.State.Config.Telegram.OwnerID &&
		ctx.EffectiveChat != nil &&
		ctx.EffectiveChat.Id == state.State.Config.Telegram.OwnerID
}

func TelegramUserStatusHandler(bot *gotgbot.Bot,ctx *ext.Context)error{
	if !isPrivateOwner(ctx){return nil}
	manager:=groupclient.Active()
	text:="A conta Telegram para criar grupos não foi configurada no Northflank."
	if manager!=nil {
		if manager.Ready(){text="Conta Telegram conectada, pronta para recuperar/criar grupos."}else{
			text="Conta Telegram configurada, aguardando autenticação. Use /tglogin."}
	}
	_,err:=utils.TgReplyTextByContext(bot,ctx,text,nil,false)
	return err
}

// TelegramUserLoginHandler starts an official QR-based MTProto login.
// Numeric sign-in codes are never sent to the bot or to a chat.
func TelegramUserLoginHandler(bot *gotgbot.Bot,ctx *ext.Context)error{
	if !isPrivateOwner(ctx){return nil}
	manager:=groupclient.Active()
	if manager==nil{
		_,err:=utils.TgReplyTextByContext(bot,ctx,
			"Configure WATG_TG_APP_ID, WATG_TG_APP_HASH e WATG_TG_PHONE como segredos no Northflank.",nil,false)
		return err
	}
	if manager.Ready(){
		_,err:=utils.TgReplyTextByContext(bot,ctx,"A conta Telegram já está autenticada.",nil,false)
		return err
	}
	if err:=manager.BeginLogin();err!=nil{
		return utils.TgReplyWithErrorByContext(bot,ctx,"Login já solicitado",err)
	}
	_,err:=utils.TgReplyTextByContext(bot,ctx,
		"Login por QR solicitado. Abra o Telegram no computador e escaneie o QR com o celular: Configurações -> Dispositivos -> Conectar dispositivo.",nil,false)
	return err
}

// TelegramUserCodeHandler is kept solely to delete old /tgcodigo messages.
// Telegram invalidates numeric login codes when sent to ANY chat.
func TelegramUserCodeHandler(bot *gotgbot.Bot, ctx *ext.Context) error {
	if !isPrivateOwner(ctx) { return nil }
	if ctx.EffectiveMessage != nil {
		_, _ = bot.DeleteMessage(ctx.EffectiveChat.Id, ctx.EffectiveMessage.MessageId, nil)
	}
	_, err := bot.SendMessage(state.State.Config.Telegram.OwnerID,
		"/tgcodigo foi desativado. O Telegram invalida códigos enviados por mensagem. Use /tglogin e escaneie o QR Code pelo celular.", nil)
	return err
}

// SyncDedicatedGroupsHandler recovers existing links before any creation.
// Defaults to zero creation, never creates hundreds of groups in one request.
func SyncDedicatedGroupsHandler(bot *gotgbot.Bot,ctx *ext.Context)error{
	if !isPrivateOwner(ctx){return nil}
	if !state.State.Config.Telegram.SeparateChats {
		_,err:=utils.TgReplyTextByContext(bot,ctx,
			"O modo de grupos separados não está ativado. Não farei alterações.",nil,false)
		return err
	}
	manager:=groupclient.Active()
	if manager==nil||!manager.Ready(){
		_,err:=utils.TgReplyTextByContext(bot,ctx,
			"Conecte sua conta Telegram primeiro: /tgstatus e /tglogin.",nil,false)
		return err
	}
	count:=0
	args:=ctx.Args()
	if len(args)>1 {
		n,err:=strconv.Atoi(args[1])
		if err!=nil || n<0||n>5 {
			_,replyErr:=utils.TgReplyTextByContext(bot,ctx,
				"Use /criarchats 1 até /criarchats 5. Para só recuperar vínculos: /criarchats 0.",nil,false)
			return replyErr
		}
		count=n
	}
	// An explicit recovery-only command must rescan Telegram even when an
	// earlier automatic batch already cached its catalog.
	if count == 0 { manager.ForceRemoteRecovery() }
	_,err:=utils.TgReplyTextByContext(bot,ctx,
		fmt.Sprintf("Recuperando os grupos existentes. Novos grupos autorizados neste lote: %d.",count),nil,false)
	if err!=nil{return err}
	go func(){
		opCtx,cancel:=context.WithTimeout(context.Background(),12*time.Minute)
		defer cancel()
		report,err:=manager.Sync(opCtx,count,bot.Username)
		message:=fmt.Sprintf("Telegram: %d vínculos identificados, %d recuperados e %d novos grupos criados.",
			report.Found,report.Recovered,report.Created)
		if err!=nil {
			// Errors can include an ID, but never include login tokens.
			message += "\nNão concluído: "+err.Error()
		}
		_,_ = bot.SendMessage(state.State.Config.Telegram.OwnerID,message,nil)
	}()
	return nil
}

var _ = errors.New
