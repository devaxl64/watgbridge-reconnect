package sessionbackup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

const backupCaption = "WATGBRIDGE_SESSION_BACKUP_V1"
const backupFilename = "watgbridge-session.enc"

var ErrNoBackup = errors.New("no encrypted session backup pinned in the Telegram owner chat")

// TelegramStore uses only Telegram's Bot API. The backup is encrypted
// BEFORE it reaches Telegram. No user MTProto login is needed to restore it.
type TelegramStore struct {
	token string
	chatID int64
	client *http.Client
	apiRoot string
	fileRoot string
}

func NewTelegramStore(token string, ownerID int64) (*TelegramStore, error) {
	if token == "" || ownerID <= 0 {
		return nil, errors.New("backup requires Telegram bot token and a private owner chat")
	}
	return &TelegramStore{
		token: token, chatID: ownerID,
		client: &http.Client{Timeout: 90 * time.Second},
		apiRoot: "https://api.telegram.org/bot",
		fileRoot: "https://api.telegram.org/file/bot",
	}, nil
}

type telegramReply struct {
	OK bool `json:"ok"`
	Description string `json:"description"`
	Result json.RawMessage `json:"result"`
}
type telegramDocument struct {
	FileID string `json:"file_id"`
	FileSize int64 `json:"file_size"`
	FileName string `json:"file_name"`
}
type telegramPinned struct {
	MessageID int64 `json:"message_id"`
	Caption string `json:"caption"`
	Document *telegramDocument `json:"document"`
}
type telegramChat struct { PinnedMessage *telegramPinned `json:"pinned_message"` }
type telegramFile struct { FilePath string `json:"file_path"`; FileSize int64 `json:"file_size"` }

func (t *TelegramStore) doAPI(ctx context.Context, method, contentType string, payload io.Reader, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.apiRoot+t.token+"/"+method, payload)
	if err != nil {
		return errors.New("could not create Telegram API request")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		// Never wrap the raw HTTP error: its URL may contain the bot token.
		return fmt.Errorf("Telegram API request %s failed", method)
	}
	defer resp.Body.Close()
	var envelope telegramReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("Telegram API %s returned unreadable response", method)
	}
	if resp.StatusCode != 200 || !envelope.OK {
		description := envelope.Description
		if len(description) > 150 {
			description = description[:150]
		}
		return fmt.Errorf("Telegram API %s error (%d): %s", method, resp.StatusCode, description)
	}
	if result != nil {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("Telegram API %s returned invalid data", method)
		}
	}
	return nil
}

func (t *TelegramStore) callJSON(ctx context.Context, method string, params any, result any) error {
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return t.doAPI(ctx, method, "application/json", bytes.NewReader(data), result)
}

func (t *TelegramStore) pinned(ctx context.Context) (*telegramPinned, error) {
	var chat telegramChat
	if err := t.callJSON(ctx, "getChat", map[string]any{"chat_id": t.chatID}, &chat); err != nil {
		return nil, err
	}
	msg := chat.PinnedMessage
	if msg == nil || (msg.Caption != backupCaption && !strings.HasPrefix(msg.Caption, backupCaption+"\n")) || msg.Document == nil || msg.Document.FileID == "" {
		return nil, ErrNoBackup
	}
	return msg, nil
}

func (t *TelegramStore) mediaRequest(ctx context.Context, method string, fields map[string]string, data []byte, result any) error {
	if len(data) == 0 || len(data) > MaxEncryptedSize {
		return errors.New("encrypted backup size is out of bounds")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile("document", backupFilename)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return t.doAPI(ctx, method, writer.FormDataContentType(), &body, result)
}

// backupPrevious is stored in the caption of the latest pinned encrypted
// document. Telegram only exposes the latest pinned message via getChat, so
// the caption retains the *previous* encrypted document's file ID.
type backupPrevious struct {
	FileID string `json:"file_id"`
	MessageID int64 `json:"message_id"`
}

func previousFromCaption(caption string) backupPrevious {
	var previous backupPrevious
	if !strings.HasPrefix(caption, backupCaption+"\n") { return previous }
	if err := json.Unmarshal([]byte(strings.TrimPrefix(caption, backupCaption+"\n")), &previous); err != nil {
		return backupPrevious{}
	}
	if len(previous.FileID) > 1024 || previous.MessageID < 0 {
		return backupPrevious{}
	}
	return previous
}

// Upload ALWAYS creates and verifies the new encrypted document before
// replacing the pinned pointer. Editing the only existing backup in place
// would destroy the last known-good restore copy if the new blob were bad.
// The two most recent blobs remain available after a successful rotation.
func (t *TelegramStore) Upload(ctx context.Context, encrypted []byte) error {
	if len(encrypted) == 0 || len(encrypted) > MaxEncryptedSize {
		return errors.New("encrypted backup exceeds size limit")
	}
	old, err := t.pinned(ctx)
	if err != nil && !errors.Is(err, ErrNoBackup) { return err }

	caption := backupCaption
	if old != nil {
		ref, err := json.Marshal(backupPrevious{FileID: old.Document.FileID, MessageID: old.MessageID})
		if err != nil { return err }
		caption += "\n" + string(ref)
		if len(caption) > 1024 {
			return errors.New("backup caption exceeds Telegram length limit")
		}
	}
	var uploaded telegramPinned
	err = t.mediaRequest(ctx, "sendDocument",
		map[string]string{
			"chat_id": strconv.FormatInt(t.chatID, 10),
			"caption": caption,
			"disable_notification": "true",
		}, encrypted, &uploaded)
	if err != nil { return err }
	if uploaded.MessageID == 0 || uploaded.Document == nil || uploaded.Document.FileID == "" {
		return errors.New("Telegram did not return the uploaded backup document")
	}
	// This verification also checks that the bot can fetch the new document
	// on restart. On failure, the old pinned backup is left untouched.
	verified, err := t.downloadFile(ctx, uploaded.Document.FileID)
	if err != nil { return fmt.Errorf("uploaded backup is not readable: %w", err) }
	if !bytes.Equal(verified, encrypted) {
		return errors.New("uploaded backup differs from the encrypted snapshot")
	}
	var pinned bool
	if err := t.callJSON(ctx, "pinChatMessage", map[string]any{
		"chat_id": t.chatID,
		"message_id": uploaded.MessageID,
		"disable_notification": true,
	}, &pinned); err != nil { return err }
	if !pinned { return errors.New("Telegram did not confirm pinning the new backup") }

	// Preserve the immediate previous backup for rollback. An older third
	// document is removed when supported; a failed cleanup is not a backup
	// failure and does not remove either of the two newest snapshots.
	if old != nil {
		grandparent := previousFromCaption(old.Caption)
		if grandparent.MessageID != 0 &&
			grandparent.MessageID != old.MessageID &&
			grandparent.MessageID != uploaded.MessageID {
			var deleted bool
			_ = t.callJSON(ctx, "deleteMessage", map[string]any{
				"chat_id": t.chatID, "message_id": grandparent.MessageID,
			}, &deleted)
		}
	}
	return nil
}

// downloadFile retrieves an encrypted document, bounded by the Bot API's
// download size limit and validated against safe relative file paths.
func (t *TelegramStore) downloadFile(ctx context.Context, fileID string) ([]byte, error) {
	if fileID == "" { return nil, errors.New("missing backup file ID") }
	var file telegramFile
	if err := t.callJSON(ctx, "getFile", map[string]any{"file_id": fileID}, &file); err != nil {
		return nil, err
	}
	if file.FilePath == "" || file.FileSize > MaxEncryptedSize ||
		strings.HasPrefix(file.FilePath, "/") || strings.Contains(file.FilePath, "..") ||
		path.Clean(file.FilePath) != file.FilePath {
		return nil, errors.New("Telegram returned invalid backup file location")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		t.fileRoot+t.token+"/"+file.FilePath, nil)
	if err != nil { return nil, errors.New("could not create Telegram file request") }
	resp, err := t.client.Do(req)
	if err != nil { return nil, errors.New("could not download encrypted backup from Telegram") }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Telegram file download failed with HTTP %d", resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, MaxEncryptedSize+1))
	if err != nil { return nil, err }
	if len(payload) == 0 || len(payload) > MaxEncryptedSize {
		return nil, errors.New("downloaded encrypted backup has invalid size")
	}
	return payload, nil
}

// DownloadCandidates retrieves the newest and previous encrypted snapshots.
// The manager authenticates/decrypts them in order, using the older one only
// when the newest has become unreadable or corrupt.
func (t *TelegramStore) DownloadCandidates(ctx context.Context) ([][]byte, error) {
	msg, err := t.pinned(ctx)
	if err != nil { return nil, err }
	previous := previousFromCaption(msg.Caption)
	ids := []string{msg.Document.FileID}
	if previous.FileID != "" && previous.FileID != msg.Document.FileID {
		ids = append(ids, previous.FileID)
	}
	var candidates [][]byte
	var lastErr error
	for _, id := range ids {
		data, err := t.downloadFile(ctx, id)
		if err != nil { lastErr = err; continue }
		candidates = append(candidates, data)
	}
	if len(candidates) == 0 {
		if lastErr == nil { lastErr = errors.New("no readable encrypted backup") }
		return nil, lastErr
	}
	return candidates, nil
}

// Download retains the simple latest-first API for diagnostic callers.
func (t *TelegramStore) Download(ctx context.Context) ([]byte, error) {
	candidates, err := t.DownloadCandidates(ctx)
	if err != nil { return nil, err }
	return candidates[0], nil
}
