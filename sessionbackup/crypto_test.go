package sessionbackup

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func exampleFiles() map[string][]byte {
	return map[string][]byte{
		"wawebstore.db": []byte("SQLite WhatsApp session test bytes"),
		"gobot.sqlite.db": []byte("SQLite bridge mapping test bytes"),
	}
}
func randomKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}
func TestSealOpenRoundTrip(t *testing.T) {
	key := randomKey(t)
	blob, err := Seal(key, exampleFiles())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte("WhatsApp")) {
		t.Fatal("plaintext visible in ciphertext")
	}
	restored, err := Open(key, blob)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range exampleFiles() {
		if !bytes.Equal(restored[name], data) {
			t.Fatalf("%s mismatch", name)
		}
	}
}
func TestWrongKeyAndTamperingFail(t *testing.T) {
	key := randomKey(t)
	blob, err := Seal(key, exampleFiles())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(randomKey(t), blob); err == nil {
		t.Fatal("wrong key accepted")
	}
	blob[len(blob)-1] ^= 0xFF
	if _, err := Open(key, blob); err == nil {
		t.Fatal("tampered backup accepted")
	}
}
func TestRestoresOnlyMissingFiles(t *testing.T) {
	dir := t.TempDir()
	paths := map[string]string{
		"wawebstore.db": filepath.Join(dir, "wawebstore.db"),
		"gobot.sqlite.db": filepath.Join(dir, "gobot.sqlite.db"),
	}
	if err := os.WriteFile(paths["wawebstore.db"], []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	count, err := RestoreMissing(exampleFiles(), paths)
	if err != nil || count != 1 {
		t.Fatalf("restored %d files, error %v", count, err)
	}
	wa, _ := os.ReadFile(paths["wawebstore.db"])
	tg, _ := os.ReadFile(paths["gobot.sqlite.db"])
	if string(wa) != "existing" || string(tg) != string(exampleFiles()["gobot.sqlite.db"]) {
		t.Fatal("overwrote existing database or did not restore missing one")
	}
}
func TestEnvironmentKey(t *testing.T) {
	key := randomKey(t)
	t.Setenv("WATG_BACKUP_KEY", base64.StdEncoding.EncodeToString(key))
	decoded, err := KeyFromEnvironment()
	if err != nil || !bytes.Equal(decoded, key) {
		t.Fatalf("key decode error: %v", err)
	}
	t.Setenv("WATG_BACKUP_KEY", "not-valid")
	if _, err := KeyFromEnvironment(); err == nil {
		t.Fatal("weak key accepted")
	}
}
