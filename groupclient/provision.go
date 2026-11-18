package groupclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"watgbridge/database"
	"watgbridge/routeidentity"
	"watgbridge/state"

	waTypes "go.mau.fi/whatsmeow/types"

	"github.com/gotd/td/tg"
)

// SupergroupID returns the Bot API chat ID for a Telegram MTProto supergroup.
func SupergroupID(channelID int64) int64 { return -1000000000000 - channelID }

type Report struct {
	Found int
	Recovered int
	Created int
	Renamed int
	Skipped int
}

var ErrPartialDialogScan = errors.New("Telegram dialog scan was incomplete; refusing to create duplicate groups")

var provisioning atomic.Bool

// Sync first reconstructs ALL known group links from encrypted group descriptions
// on Telegram's servers. Only after a complete scan may it create groups.
// The group creation count is small to avoid Telegram flooding/banning.
func (m *Manager) Sync(ctx context.Context, maxNew int, botUsername string) (Report, error) {
	var report Report
	if !m.Ready() { return report, ErrNotAuthorized }
	if !provisioning.CompareAndSwap(false,true) {
		return report, errors.New("a Telegram group sync is already running")
	}
	defer provisioning.Store(false)
	if maxNew < 0 { maxNew=0 }
	if maxNew > 5 { maxNew=5 }
	if botUsername == "" { return report, errors.New("Telegram bot username missing") }

	err:=m.Do(ctx,func(ctx context.Context,api *tg.Client)error{
		if !m.remoteCatalogReady.Load() {
			found,err:=scanTelegramBridgeGroups(ctx,api)
			if err!=nil{return err} // never create groups on incomplete scan
			report.Found=len(found)
			for waID, ch:=range found {
			kind:="private"
			if strings.HasSuffix(waID,"@g.us"){kind="group"}
			// Old versions could create a group for placeholder chat ID "0".
			// Preserve that Telegram group; don't restore it as a usable route.
			if !database.IsActionableKnownChat(database.KnownChat{WaChatID:waID,Kind:kind}) {
				report.Skipped++
				continue
			}
			_,wasLinked,lookupErr:=database.TelegramGroupForWhatsApp(waID)
			if lookupErr!=nil{return lookupErr}
			if err:=database.EnsureSeparateChat(waID,SupergroupID(ch.ID));err!=nil {
				return fmt.Errorf("recovery conflict for %s: %w",waID,err)
			}
			if err:=database.RecordKnownChat(database.KnownChat{
				WaChatID:waID,Kind:kind,Source:"telegram-recovery",
			});err!=nil{return err}
				if !wasLinked {report.Recovered++}
			}
			m.remoteCatalogReady.Store(true)
		}
		if maxNew==0{return nil}

		chats,err:=database.ListAllKnownChats(0)
		if err!=nil{return err}
		// Repair up to two automatically generated numeric group titles on
		// each sync. Respect any human-edited Telegram group titles.
		for _,chat:=range chats {
			if report.Renamed>=2 { break }
			if ctx.Err()!=nil{return ctx.Err()}
			if chat.Kind!="private" {continue}
			number:=strings.SplitN(chat.WaChatID,"@",2)[0]
			name:=strings.TrimSpace(chat.Name)
			if !humanContactName(name,number) {name=contactDisplayName(chat)}
			if name=="" {continue}
			tgID,linked,lookupErr:=database.TelegramGroupForWhatsApp(chat.WaChatID)
			if lookupErr!=nil{return lookupErr}
			if !linked || state.State.TelegramBot==nil {continue}
			remote,lookupErr:=state.State.TelegramBot.GetChat(tgID,nil)
			if lookupErr!=nil {continue} // no permission: never break creation
			if !isLegacyNumericTitle(remote.Title,number) {continue}
			expected:=chatTitle(database.KnownChat{WaChatID:chat.WaChatID,Kind:chat.Kind,Name:name})
			if _,renameErr:=state.State.TelegramBot.SetChatTitle(tgID,expected,nil);renameErr==nil {
				report.Renamed++
				_ = database.RecordKnownChat(database.KnownChat{WaChatID:chat.WaChatID,Kind:chat.Kind,Name:name,Source:"contact-title-refresh"})
			}
		}
		for _,chat:=range chats {
			if report.Created>=maxNew {break}
			if ctx.Err()!=nil{return ctx.Err()}
			if !database.IsActionableKnownChat(chat) {report.Skipped++;continue}
			_,linked,err:=database.TelegramGroupForWhatsApp(chat.WaChatID)
			if err!=nil{return err}
			if linked {report.Skipped++;continue}
			// A private LID without a verified phone-number mapping may
			// later resolve to a PN and duplicate the same human contact.
			if strings.HasSuffix(chat.WaChatID,"@lid") {report.Skipped++;continue}
			// WhatsApp's LID identifiers do not necessarily contain a phone;
			// never guess the number.
			encoded,err:=routeidentity.Encrypt(chat.WaChatID)
			if err!=nil{return err}
			title:=chatTitle(chat)
			channel,err:=createPrivateSupergroup(ctx,api,title,encoded)
			if err!=nil{return fmt.Errorf("could not create Telegram group %q: %w",title,err)}
			// The encrypted WhatsApp JID is already saved server-side in
			// the new group's description; a crash here will be recoverable.
			if err:=ensureBotAdmin(ctx,api,channel,botUsername);err!=nil{
				return fmt.Errorf("Telegram group created but the bot could not join as admin: %w",err)
			}
			if err:=database.EnsureSeparateChat(chat.WaChatID,SupergroupID(channel.ID));err!=nil{
				return err
			}
			report.Created++
			// One new group every ten seconds. The administrator may start
			// another small batch only after the current one finishes.
			if report.Created<maxNew {
				select {
				case <-ctx.Done():return ctx.Err()
				case <-time.After(10*time.Second):
				}
			}
		}
		return nil
	})
	// An error after creating a remote group may mean its local route was
	// not saved. Force a remote rescan on retry before another creation.
	if err != nil { m.remoteCatalogReady.Store(false) }
	return report,err
}

// contactDisplayName checks the real WhatsApp contact store and its cached
// contacts. The chat ID is a routing key, never a name; LID IDs are not phones.
func contactDisplayName(chat database.KnownChat) string {
	if chat.Kind != "private" { return "" }
	jid, err := waTypes.ParseJID(chat.WaChatID)
	if err != nil || jid.Server != waTypes.DefaultUserServer || jid.User == "" {
		return ""
	}
	wa := state.State.WhatsAppClient
	if wa != nil && wa.Store != nil && wa.Store.Contacts != nil {
		lookupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		contact, getErr := wa.Store.Contacts.GetContact(lookupCtx, jid)
		cancel()
		if getErr == nil && contact.Found {
			for _, name := range []string{contact.FullName, contact.BusinessName, contact.FirstName, contact.PushName} {
				name = strings.TrimSpace(name)
				if humanContactName(name, jid.User) { return name }
			}
		}
	}
	if state.State.Database != nil {
		first, full, push, business, found, getErr := database.ContactNameGet(jid.User, jid.Server)
		if getErr == nil && found {
			for _, name := range []string{full, business, first, push} {
				name = strings.TrimSpace(name)
				if humanContactName(name, jid.User) { return name }
			}
		}
	}
	return ""
}

func isLegacyNumericTitle(title, number string) bool {
	title=strings.TrimSpace(title)
	return title==number || title=="+"+number || title==strings.TrimPrefix(number,"55")
}

func humanContactName(name, number string) bool {
	name = strings.TrimSpace(name)
	return name != "" && name != number && name != "+"+number &&
		name != strings.TrimPrefix(number, "55") && name != "0"
}

// chatTitle prefers the WhatsApp address-book name. For a known phone-number
// JID without a saved name, display it as an international phone number (+DDI)
// rather than a bare numeric database identifier.
func chatTitle(chat database.KnownChat) string {
	name := strings.TrimSpace(chat.Name)
	num := chat.WaChatID
	if at := strings.IndexByte(num, '@'); at >= 0 { num = num[:at] }
	if chat.Kind == "private" {
		if !humanContactName(name, num) {
			name = contactDisplayName(chat)
		}
		if name == "" {
			name = "+" + num
		} else if !strings.HasSuffix(name, " (+"+num+")") {
			name += " (+" + num + ")"
		}
	} else if name == "" {
		name = "Grupo " + num
	}
	if utf8.RuneCountInString(name) > 90 {
		name = string([]rune(name)[:90])
	}
	return name
}

func createPrivateSupergroup(ctx context.Context,api *tg.Client,title,about string)(*tg.Channel,error){
	updates,err:=api.ChannelsCreateChannel(ctx,&tg.ChannelsCreateChannelRequest{
		Megagroup:true,Forum:false,Title:title,About:about,
	})
	if err!=nil{return nil,err}
	var chats []tg.ChatClass
	switch u:=updates.(type){
	case *tg.Updates:
		chats=u.Chats
	case *tg.UpdatesCombined:
		chats=u.Chats
	default:
		return nil,errors.New("Telegram created group but returned no channel metadata")
	}
	for _,entry:=range chats {
		if channel,ok:=entry.(*tg.Channel);ok&&channel.Megagroup {
			return channel,nil
		}
	}
	return nil,errors.New("Telegram created group but did not return its channel ID")
}

func ensureBotAdmin(ctx context.Context,api *tg.Client,channel *tg.Channel,username string)error{
	resolved,err:=api.ContactsResolveUsername(ctx,&tg.ContactsResolveUsernameRequest{
		Username:strings.TrimPrefix(username,"@"),
	})
	if err!=nil{return err}
	var bot *tg.User
	for _,entry:=range resolved.Users{
		if user,ok:=entry.(*tg.User);ok&&user.Bot {
			bot=user
			break
		}
	}
	if bot==nil{return errors.New("resolved username is not a Telegram bot")}
	inputChannel:=&tg.InputChannel{ChannelID:channel.ID,AccessHash:channel.AccessHash}
	inputBot:=&tg.InputUser{UserID:bot.ID,AccessHash:bot.AccessHash}
	if _,err:=api.ChannelsInviteToChannel(ctx,&tg.ChannelsInviteToChannelRequest{
		Channel:inputChannel,Users:[]tg.InputUserClass{inputBot},
	});err!=nil && !strings.Contains(strings.ToUpper(err.Error()),"USER_ALREADY_PARTICIPANT"){
		return err
	}
	_,err=api.ChannelsEditAdmin(ctx,&tg.ChannelsEditAdminRequest{
		Channel:inputChannel,
		UserID:inputBot,
		AdminRights:tg.ChatAdminRights{
			ChangeInfo:true,DeleteMessages:true,InviteUsers:true,PinMessages:true,Other:true,
		},
	})
	return err
}

// scanTelegramBridgeGroups scans all dialog pages instead of relying on a
// local SQLite route table. Its conservative failure behavior is intentional:
// if the Telegram list cannot be exhausted, NO new group will be created.
func scanTelegramBridgeGroups(ctx context.Context,api *tg.Client)(map[string]*tg.Channel,error){
	found:=map[string]*tg.Channel{}
	req:=&tg.MessagesGetDialogsRequest{
		Limit:100,OffsetPeer:&tg.InputPeerEmpty{},
	}
	// Never cap recovery at 50 pages: accounts with many dialogs need full
	// traversal. The caller's context timeout is the operational boundary.
	for {
		if err := ctx.Err(); err != nil { return nil, err }
		res,err:=api.MessagesGetDialogs(ctx,req)
		if err!=nil{return nil,err}
		var dialogs []tg.DialogClass
		var chats []tg.ChatClass
		var users []tg.UserClass
		var messages []tg.MessageClass
		switch d:=res.(type){
		case *tg.MessagesDialogs:
			dialogs,chats,users,messages=d.Dialogs,d.Chats,d.Users,d.Messages
		case *tg.MessagesDialogsSlice:
			dialogs,chats,users,messages=d.Dialogs,d.Chats,d.Users,d.Messages
		case *tg.MessagesDialogsNotModified:
			return nil,ErrPartialDialogScan
		default:return nil,fmt.Errorf("unrecognized Telegram dialogs response %T",res)
		}
		for _,c:=range chats {
			ch,ok:=c.(*tg.Channel)
			if !ok||!ch.Megagroup||ch.AccessHash==0 {continue}
			full,err:=api.ChannelsGetFullChannel(ctx,
				&tg.InputChannel{ChannelID:ch.ID,AccessHash:ch.AccessHash})
			if err!=nil {return nil,fmt.Errorf("could not inspect Telegram group %d; no groups created: %w",ch.ID,err)}
			info,ok:=full.FullChat.(*tg.ChannelFull)
			if !ok {continue}
			waID,isBridge,err:=routeidentity.Decrypt(info.About)
			if err!=nil {
				// Never create groups until every suspected bridge description
				// has been read and authenticated successfully.
				if isBridge {return nil,err}
				continue
			}
			if !isBridge{continue}
			if previous,duplicate:=found[waID];duplicate&&previous.ID!=ch.ID{
				return nil,fmt.Errorf("two Telegram groups claim the same WhatsApp conversation: %s",waID)
			}
			found[waID]=ch
		}
		if len(dialogs)<100{return found,nil}
		// Paginate using the last dialog's top message and peer.
		var last *tg.Dialog
		for i:=len(dialogs)-1;i>=0;i--{
			if d,ok:=dialogs[i].(*tg.Dialog);ok {last=d;break}
		}
		if last==nil {return nil,ErrPartialDialogScan}
		peer,err:=dialogInputPeer(last.Peer,chats,users)
		if err!=nil{return nil,err}
		date:=0
		for _,entry:=range messages {
			switch msg:=entry.(type) {
			case *tg.Message:
				if msg.ID==last.TopMessage{date=msg.Date}
			case *tg.MessageService:
				if msg.ID==last.TopMessage{date=msg.Date}
			}
		}
		if date==0 {return nil,ErrPartialDialogScan}
		req.OffsetPeer=peer
		req.OffsetID=last.TopMessage
		req.OffsetDate=date
	}
	return nil,ErrPartialDialogScan
}

func dialogInputPeer(p tg.PeerClass,chats []tg.ChatClass,users []tg.UserClass)(tg.InputPeerClass,error){
	switch peer:=p.(type){
	case *tg.PeerChannel:
		for _,entry:=range chats{
			if ch,ok:=entry.(*tg.Channel);ok&&ch.ID==peer.ChannelID&&ch.AccessHash!=0{
				return &tg.InputPeerChannel{ChannelID:ch.ID,AccessHash:ch.AccessHash},nil
			}
		}
	case *tg.PeerChat:
		return &tg.InputPeerChat{ChatID:peer.ChatID},nil
	case *tg.PeerUser:
		for _,entry:=range users{
			if user,ok:=entry.(*tg.User);ok&&user.ID==peer.UserID&&user.AccessHash!=0{
				return &tg.InputPeerUser{UserID:user.ID,AccessHash:user.AccessHash},nil
			}
		}
	}
	return nil,ErrPartialDialogScan
}
