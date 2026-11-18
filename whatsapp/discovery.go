package whatsapp

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"watgbridge/database"
	"watgbridge/state"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.uber.org/zap"
)

// A chat is discovered from the WhatsApp history-sync CHAT LIST, not from
// importing historical message bodies or media into our server.
func canonicalChatID(raw string) (id, kind string, ok bool) {
	jid, err := waTypes.ParseJID(strings.TrimSpace(raw))
	if err != nil || jid.User == "" {
		return "", "", false
	}
	jid = CanonicalChatJID(jid)
	switch jid.Server {
	case "g.us":
		return jid.String(), "group", true
	case "s.whatsapp.net", "lid":
		return jid.String(), "private", true
	default:
		return "", "", false // ignore status, broadcasts and newsletters
	}
}

func recordChatID(rawID, name, source string) {
	id, kind, ok := canonicalChatID(rawID)
	if !ok {
		return
	}
	if err := database.RecordKnownChat(database.KnownChat{
		WaChatID: id,
		Name: strings.TrimSpace(name),
		Kind: kind,
		Source: source,
		SeenAt: time.Now().UTC(),
	}); err != nil && state.State.Logger != nil {
		state.State.Logger.Warn("failed to record WhatsApp chat metadata", zap.Error(err))
	}
}

// CaptureHistorySync reads conversation IDs and display names only. It does
// not inspect or store the messages contained in the sync event.
func CaptureHistorySync(event *events.HistorySync) {
	if event == nil || event.Data == nil {
		return
	}
	count := 0
	for _, conversation := range event.Data.GetConversations() {
		if conversation == nil {
			continue
		}
		id, kind, ok := canonicalChatID(conversation.GetID())
		if !ok {
			continue
		}
		if err := database.RecordKnownChat(database.KnownChat{
			WaChatID: id,
			Name: strings.TrimSpace(conversation.GetDisplayName()),
			Kind: kind,
			Source: "history-sync",
		}); err != nil {
			if state.State.Logger != nil {
				state.State.Logger.Warn("could not save WhatsApp chat from history sync", zap.Error(err))
			}
			continue
		}
		count++
	}
	if state.State.Logger != nil {
		state.State.Logger.Info("WhatsApp chat discovery completed from history metadata",
			zap.Int("conversation_entries_recorded", count))
	}
}

// CaptureIncomingChat retains only the sender chat's identity. Group sender
// nicknames aren't used as group titles. No message body is saved here.
func CaptureIncomingChat(event *events.Message) {
	if event == nil {
		return
	}
	name := ""
	if !event.Info.IsFromMe && event.Info.Chat.Server != "g.us" {
		name = event.Info.PushName
	}
	recordChatID(CanonicalChatJID(event.Info.Chat).String(), name, "message")
}

// CollectJoinedGroups retrieves current group IDs and titles from WhatsApp.
// Contacts who are not in the history are NOT invented as conversations.
func CollectJoinedGroups(ctx context.Context, client *whatsmeow.Client) (int, error) {
	if client == nil || !client.IsLoggedIn() {
		return 0, nil
	}
	groups, err := client.GetJoinedGroups(ctx)
	if err != nil {
		return 0, err
	}
	saved := 0
	for _, group := range groups {
		if group == nil {
			continue
		}
		id, kind, ok := canonicalChatID(group.JID.String())
		if !ok || kind != "group" {
			continue
		}
		if err := database.RecordKnownChat(database.KnownChat{
			WaChatID: id, Name: group.Name, Kind: "group", Source: "joined-groups",
		}); err != nil {
			return saved, err
		}
		saved++
	}
	return saved, nil
}

// A burst of reconnects must not trigger concurrent group enumerations.
var groupDiscoveryActive atomic.Bool

func StartJoinedGroupDiscovery() {
	if !groupDiscoveryActive.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer groupDiscoveryActive.Store(false)
		// The WhatsApp connection may have just emitted Connected and still
		// be finalizing authentication or app-state syncing.
		time.Sleep(3 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		count, err := CollectJoinedGroups(ctx, state.State.WhatsAppClient)
		if state.State.Logger == nil {
			return
		}
		if err != nil {
			state.State.Logger.Warn("WhatsApp group discovery did not complete", zap.Error(err))
		} else {
			state.State.Logger.Info("WhatsApp group metadata synchronized",
				zap.Int("groups", count))
		}
	}()
}
