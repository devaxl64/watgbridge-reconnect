package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
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

// TelegramUserLoginHandler starts a one-time user login. Login codes must
// never appear in logs, config files, GitHub or public groups.
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
		"Login solicitado. Aguarde a mensagem do código e use /tgcodigo 12345 no privado.",nil,false)
	return err
}

// TelegramUserCodeHandler deletes the plaintext login code immediately
// from chat history and submits it only to the currently waiting login flow.
func TelegramUserCodeHandler(bot *gotgbot.Bot,ctx *ext.Context)error{
	if !isPrivateOwner(ctx){return nil}
	if ctx.EffectiveMessage!=nil {
		_,_ = bot.DeleteMessage(ctx.EffectiveChat.Id,ctx.EffectiveMessage.MessageId,nil)
	}
	manager:=groupclient.Active()
	if manager==nil {
		_,err:=bot.SendMessage(state.State.Config.Telegram.OwnerID,
			"Conta Telegram não configurada. Use /tgstatus.",nil)
		return err
	}
	args:=ctx.Args()
	if len(args)!=2 {
		_,err:=bot.SendMessage(state.State.Config.Telegram.OwnerID,
			"Formato: /tgcodigo 12345. A mensagem é apagada depois de lida.",nil)
		return err
	}
	if err:=manager.SubmitCode(strings.TrimSpace(args[1]));err!=nil {
		return utils.TgReplyWithErrorByContext(bot,ctx,"Código não solicitado",err)
	}
	_,err:=bot.SendMessage(state.State.Config.Telegram.OwnerID,
		"Código enviado para autenticação. Aguarde a confirmação.",nil)
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
