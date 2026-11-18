package sessionbackup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestTelegramEncryptedPinnedBackupLifecycle(t *testing.T) {
	type document struct {
		id int64
		fileID string
		caption string
		bytes []byte
	}
	var mu sync.Mutex
	docs := map[string]*document{}
	var pinnedID int64
	var nextID int64 = 15
	var sendCount, deleteCount int

	ok := func(w http.ResponseWriter, value any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": value})
	}
	result := func(d *document) map[string]any {
		return map[string]any{
			"message_id": d.id, "caption": d.caption,
			"document": map[string]any{
				"file_id": d.fileID, "file_size": len(d.bytes), "file_name": backupFilename,
			},
		}
	}
	findByID := func(id int64) *document {
		for _, d := range docs {
			if d.id == id { return d }
		}
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/botfake/getChat", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); defer mu.Unlock()
		if d := findByID(pinnedID); d != nil {
			ok(w, map[string]any{"pinned_message": result(d)})
		} else {
			ok(w, map[string]any{})
		}
	})
	mux.HandleFunc("/botfake/sendDocument", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); defer mu.Unlock()
		if err := r.ParseMultipartForm(MaxEncryptedSize + 1024); err != nil { t.Error(err); return }
		file, _, err := r.FormFile("document")
		if err != nil { t.Error(err); return }
		defer file.Close()
		body, err := io.ReadAll(file)
		if err != nil { t.Error(err); return }
		nextID++
		id := "doc-" + strconv.FormatInt(nextID, 10)
		d := &document{id: nextID, fileID: id, caption: r.FormValue("caption"), bytes: body}
		docs[id] = d
		sendCount++
		ok(w, result(d))
	})
	mux.HandleFunc("/botfake/pinChatMessage", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); defer mu.Unlock()
		var request struct{ MessageID int64 `json:"message_id"` }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil { t.Error(err); return }
		if findByID(request.MessageID) == nil { t.Error("pinning missing document"); return }
		pinnedID = request.MessageID
		ok(w, true)
	})
	mux.HandleFunc("/botfake/getFile", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); defer mu.Unlock()
		var request struct{ FileID string `json:"file_id"` }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil { t.Error(err); return }
		d := docs[request.FileID]
		if d == nil { http.Error(w, "missing file", http.StatusNotFound); return }
		ok(w, map[string]any{"file_path": "documents/" + d.fileID + ".enc", "file_size": len(d.bytes)})
	})
	mux.HandleFunc("/botfake/deleteMessage", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); defer mu.Unlock()
		var request struct{ MessageID int64 `json:"message_id"` }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil { t.Error(err); return }
		for id, d := range docs {
			if d.id == request.MessageID {
				delete(docs, id)
				deleteCount++
				break
			}
		}
		ok(w, true)
	})
	mux.HandleFunc("/file/botfake/documents/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); defer mu.Unlock()
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/file/botfake/documents/"), ".enc")
		if d := docs[id]; d != nil {
			_, _ = w.Write(d.bytes)
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	store, err := NewTelegramStore("fake", 8256480955)
	if err != nil { t.Fatal(err) }
	store.apiRoot = srv.URL + "/bot"
	store.fileRoot = srv.URL + "/file/bot"
	store.client = srv.Client()
	ctx := context.Background()
	if _, err := store.Download(ctx); !errorsIs(err, ErrNoBackup) {
		t.Fatalf("expected no backup, got %v", err)
	}
	key := randomKey(t)
	first, err := Seal(key, exampleFiles())
	if err != nil { t.Fatal(err) }
	if err := store.Upload(ctx, first); err != nil { t.Fatal(err) }
	if got, err := store.Download(ctx); err != nil || !bytes.Equal(first, got) {
		t.Fatalf("first backup mismatch: %v", err)
	}
	second, err := Seal(key, exampleFiles())
	if err != nil { t.Fatal(err) }
	if err := store.Upload(ctx, second); err != nil { t.Fatal(err) }
	candidates, err := store.DownloadCandidates(ctx)
	if err != nil || len(candidates) != 2 ||
		!bytes.Equal(candidates[0], second) || !bytes.Equal(candidates[1], first) {
		t.Fatalf("latest and previous backups were not retained: %v", err)
	}
	third, err := Seal(key, exampleFiles())
	if err != nil { t.Fatal(err) }
	if err := store.Upload(ctx, third); err != nil { t.Fatal(err) }
	candidates, err = store.DownloadCandidates(ctx)
	if err != nil || len(candidates) != 2 ||
		!bytes.Equal(candidates[0], third) || !bytes.Equal(candidates[1], second) {
		t.Fatalf("backup rotation invalid: %v", err)
	}
	if sendCount != 3 || deleteCount != 1 {
		t.Fatalf("expected three uploads and one old cleanup, sends=%d cleanup=%d", sendCount, deleteCount)
	}
	// Simulate corruption of the latest stored blob. The previous copy
	// must still decrypt with the original key.
	mu.Lock()
	if newest := findByID(pinnedID); newest != nil { newest.bytes = []byte("corrupt") }
	mu.Unlock()
	candidates, err = store.DownloadCandidates(ctx)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("missing rollback candidate: %v", err)
	}
	if _, err := Open(key, candidates[0]); err == nil {
		t.Fatal("corrupt newest backup passed authentication")
	}
	if _, err := Open(key, candidates[1]); err != nil {
		t.Fatalf("the previous backup must remain decryptable: %v", err)
	}
}

func errorsIs(got, want error) bool { return got == want }

func TestTelegramHTTPFailureDoesNotLeakToken(t *testing.T) {
	store, err := NewTelegramStore("SECRET_TOKEN", 1)
	if err != nil { t.Fatal(err) }
	store.apiRoot = "http://127.0.0.1:1/bot"
	_, err = store.Download(context.Background())
	if err == nil || strings.Contains(fmt.Sprint(err), "SECRET_TOKEN") {
		t.Fatal("network error exposed bot token or incorrectly succeeded")
	}
}
