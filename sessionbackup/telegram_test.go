package sessionbackup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestTelegramEncryptedPinnedBackupLifecycle(t *testing.T) {
	var mu sync.Mutex
	var document []byte
	var pinned bool
	var sendCount, editCount int
	const fileID = "backup-document"
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, value any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": value})
	}
	msg := func() map[string]any {
		return map[string]any{"message_id": 15, "caption": backupCaption,
			"document": map[string]any{"file_id": fileID, "file_size": len(document), "file_name": backupFilename}}
	}
	mux.HandleFunc("/botfake/getChat", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); defer mu.Unlock()
		if !pinned { ok(w, map[string]any{}) } else { ok(w, map[string]any{"pinned_message": msg()}) }
	})
	mux.HandleFunc("/botfake/sendDocument", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); defer mu.Unlock()
		_ = r.ParseMultipartForm(MaxEncryptedSize + 1024)
		file, _, err := r.FormFile("document"); if err != nil { t.Error(err); return }
		defer file.Close()
		document, err = io.ReadAll(file); if err != nil { t.Error(err); return }
		sendCount++
		ok(w, msg())
	})
	mux.HandleFunc("/botfake/editMessageMedia", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); defer mu.Unlock()
		_ = r.ParseMultipartForm(MaxEncryptedSize + 1024)
		if !strings.Contains(r.FormValue("media"), backupCaption) { t.Error("backup caption missing") }
		file, _, err := r.FormFile("document"); if err != nil { t.Error(err); return }
		defer file.Close()
		document, err = io.ReadAll(file); if err != nil { t.Error(err); return }
		editCount++
		ok(w, msg())
	})
	mux.HandleFunc("/botfake/pinChatMessage", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); defer mu.Unlock()
		pinned = true
		ok(w, true)
	})
	mux.HandleFunc("/botfake/getFile", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); defer mu.Unlock()
		ok(w, map[string]any{"file_path": "documents/session.enc", "file_size": len(document)})
	})
	mux.HandleFunc("/file/botfake/documents/session.enc", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); defer mu.Unlock()
		w.Write(document)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	store, err := NewTelegramStore("fake", 8256480955)
	if err != nil { t.Fatal(err) }
	store.apiRoot = srv.URL + "/bot"
	store.fileRoot = srv.URL + "/file/bot"
	store.client = srv.Client()

	ctx := context.Background()
	if _, err := store.Download(ctx); err != ErrNoBackup {
		t.Fatalf("expected no backup, got %v", err)
	}
	first, err := Seal(randomKey(t), exampleFiles()); if err != nil { t.Fatal(err) }
	if err := store.Upload(ctx, first); err != nil { t.Fatal(err) }
	downloaded, err := store.Download(ctx)
	if err != nil || !bytes.Equal(downloaded, first) {
		t.Fatalf("backup mismatch: %v", err)
	}
	next, err := Seal(randomKey(t), exampleFiles()); if err != nil { t.Fatal(err) }
	if err := store.Upload(ctx, next); err != nil { t.Fatal(err) }
	if sendCount != 1 || editCount != 1 {
		t.Fatalf("send count = %d, edit count = %d", sendCount, editCount)
	}
	downloaded, err = store.Download(ctx)
	if err != nil || !bytes.Equal(downloaded, next) {
		t.Fatalf("updated backup mismatch: %v", err)
	}
}

func TestTelegramHTTPFailureDoesNotLeakToken(t *testing.T) {
	store, err := NewTelegramStore("SECRET_TOKEN", 1)
	if err != nil { t.Fatal(err) }
	store.apiRoot = "http://127.0.0.1:1/bot"
	_, err = store.Download(context.Background())
	if err == nil || strings.Contains(fmt.Sprint(err), "SECRET_TOKEN") {
		t.Fatal("network error exposed bot token or incorrectly succeeded")
	}
}
