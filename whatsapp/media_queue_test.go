package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

func TestMediaPriorityPreservesTextAndServiceMessages(t *testing.T) {
	if got:=mediaPriority(&waE2E.Message{});got!=-1{t.Fatalf("empty message got %d",got)}
	if got:=mediaPriority(&waE2E.Message{ProtocolMessage:&waE2E.ProtocolMessage{}});got!=-1{
		t.Fatalf("protocol event should not be delayed: %d",got)
	}
	if got:=mediaPriority(&waE2E.Message{ImageMessage:&waE2E.ImageMessage{}});got!=0{t.Fatalf("photo %d",got)}
	if got:=mediaPriority(&waE2E.Message{DocumentMessage:&waE2E.DocumentMessage{}});got!=1{t.Fatalf("document %d",got)}
	if got:=mediaPriority(&waE2E.Message{VideoMessage:&waE2E.VideoMessage{}});got!=2{t.Fatalf("video %d",got)}
	if got:=mediaPriority(&waE2E.Message{AudioMessage:&waE2E.AudioMessage{}});got!=1{t.Fatalf("audio %d",got)}
}

func TestStickerPriorityAndImmediateNotice(t *testing.T) {
	sticker := &waE2E.Message{StickerMessage: &waE2E.StickerMessage{}}
	if got := mediaPriority(sticker); got != 0 {
		t.Fatalf("stickers should be prioritized before documents: got %d", got)
	}
	name, caption := mediaDescription(sticker)
	if name != "Figurinha" || caption != "" {
		t.Fatalf("incorrect immediate sticker notice: %q %q", name, caption)
	}
	doc := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{}}
	if got := mediaPriority(doc); got <= mediaPriority(sticker) {
		t.Fatal("documents must wait behind pending stickers/photos")
	}
}

func TestPendingReservationDoesNotDuplicate(t *testing.T) {
	const key = "chatid\\x00message-id"
	if !reservePendingMedia(key) {
		t.Fatal("could not reserve first occurrence")
	}
	if reservePendingMedia(key) {
		t.Fatal("duplicate media event was reserved twice")
	}
	markPendingMediaDelivered(key)
	clearPendingMedia(key)
	if !reservePendingMedia(key) {
		t.Fatal("reservation was not freed after completion")
	}
	clearPendingMedia(key)
}
