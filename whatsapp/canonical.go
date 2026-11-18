package whatsapp

import (
	"context"
	"time"

	"watgbridge/state"

	waTypes "go.mau.fi/whatsmeow/types"
)

// CanonicalChatJID resolves WhatsApp's private LID IDs to their real phone
// JID when the account already knows the mapping. It never invents a number.
// This prevents the same person from getting two Telegram groups when WhatsApp
// alternates between a phone-based and a private LID-based event.
func CanonicalChatJID(jid waTypes.JID) waTypes.JID {
	jid=jid.ToNonAD()
	if jid.Server!=waTypes.HiddenUserServer{return jid}
	client:=state.State.WhatsAppClient
	if client==nil||client.Store==nil||client.Store.LIDs==nil{return jid}
	ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second)
	defer cancel()
	pn,err:=client.Store.LIDs.GetPNForLID(ctx,jid)
	if err==nil&&pn.User!=""&&pn.Server==waTypes.DefaultUserServer {
		return pn.ToNonAD()
	}
	return jid
}
