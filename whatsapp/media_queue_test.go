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
	if got:=mediaPriority(&waE2E.Message{ImageMessage:&waE2E.ImageMessage{}});got!=1{t.Fatalf("photo %d",got)}
	if got:=mediaPriority(&waE2E.Message{DocumentMessage:&waE2E.DocumentMessage{}});got!=0{t.Fatalf("document %d",got)}
	if got:=mediaPriority(&waE2E.Message{VideoMessage:&waE2E.VideoMessage{}});got!=2{t.Fatalf("video %d",got)}
	if got:=mediaPriority(&waE2E.Message{AudioMessage:&waE2E.AudioMessage{}});got!=0{t.Fatalf("audio %d",got)}
}
