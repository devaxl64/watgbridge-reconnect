package whatsapp

import (
	"testing"
	"time"
)

func TestUnlinkedMediaWarningThrottlesPerChat(t *testing.T) {
	unlinkedMediaAlerts.Lock()
	unlinkedMediaAlerts.seen = make(map[string]time.Time)
	unlinkedMediaAlerts.Unlock()
	now := time.Date(2026, 11, 18, 6, 0, 0, 0, time.UTC)
	if !shouldNotifyUnlinkedMedia("first@s.whatsapp.net", "", now) {
		t.Fatal("first media-only message should notify")
	}
	if shouldNotifyUnlinkedMedia("first@s.whatsapp.net", "", now.Add(time.Minute)) {
		t.Fatal("repeated media notice should not flood the owner")
	}
	if !shouldNotifyUnlinkedMedia("second@s.whatsapp.net", "", now.Add(time.Minute)) {
		t.Fatal("different chat still needs its first notice")
	}
	if !shouldNotifyUnlinkedMedia("first@s.whatsapp.net", "Texto importante", now.Add(time.Minute)) {
		t.Fatal("real text should still be forwarded")
	}
	if !shouldNotifyUnlinkedMedia("first@s.whatsapp.net", "", now.Add(16*time.Minute)) {
		t.Fatal("media notice should be permitted after the throttle interval")
	}
}
