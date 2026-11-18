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
	if msg == nil || msg.Caption != backupCaption || msg.Document == nil || msg.Document.FileID == "" {
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

// Upload replaces the pinned backup document in place when possible,
// avoiding duplicate messages. If editing fails, a new pinned backup is
// created; the previous pinned backup is never proactively deleted.
func (t *TelegramStore) Upload(ctx context.Context, encrypted []byte) error {
	if len(encrypted) == 0 || len(encrypted) > MaxEncryptedSize {
		return errors.New("encrypted backup exceeds size limit")
	}
	old, err := t.pinned(ctx)
	if err != nil && !errors.Is(err, ErrNoBackup) {
		return err
	}
	if old != nil {
		media := map[string]string{
			"type": "document", "media": "attach://document", "caption": backupCaption,
		}
		mediaJSON, err := json.Marshal(media)
		if err != nil {
			return err
		}
		var edited telegramPinned
		err = t.mediaRequest(ctx, "editMessageMedia",
			map[string]string{
				"chat_id": strconv.FormatInt(t.chatID, 10),
				"message_id": strconv.FormatInt(old.MessageID, 10),
				"media": string(mediaJSON),
			}, encrypted, &edited)
		if err == nil && edited.Document != nil {
			return nil
		}
		// Editing failed; preserve the old pinned copy and try a new one.
	}
	var message telegramPinned
	err = t.mediaRequest(ctx, "sendDocument",
		map[string]string{
			"chat_id": strconv.FormatInt(t.chatID, 10),
			"caption": backupCaption,
			"disable_notification": "true",
		}, encrypted, &message)
	if err != nil {
		return err
	}
	if message.MessageID == 0 {
		return errors.New("Telegram did not return message ID for uploaded backup")
	}
	var pinned bool
	if err := t.callJSON(ctx, "pinChatMessage", map[string]any{
		"chat_id": t.chatID,
		"message_id": message.MessageID,
		"disable_notification": true,
	}, &pinned); err != nil {
		return err
	}
	if !pinned {
		return errors.New("Telegram did not confirm pinning the backup")
	}
	return nil
}

// Download fetches only the document currently pinned by the bot in its
// owner's private chat. Telegram's getFile limit is 20 MB.
func (t *TelegramStore) Download(ctx context.Context) ([]byte, error) {
	pinned, err := t.pinned(ctx)
	if err != nil {
		return nil, err
	}
	if pinned.Document.FileSize > MaxEncryptedSize {
		return nil, errors.New("pinned backup is larger than the supported limit")
	}
	var file telegramFile
	if err := t.callJSON(ctx, "getFile",
		map[string]any{"file_id": pinned.Document.FileID}, &file); err != nil {
		return nil, err
	}
	if file.FilePath == "" || file.FileSize > MaxEncryptedSize ||
		strings.HasPrefix(file.FilePath, "/") || strings.Contains(file.FilePath, "..") ||
		path.Clean(file.FilePath) != file.FilePath {
		return nil, errors.New("Telegram returned invalid backup file location")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		t.fileRoot+t.token+"/"+file.FilePath, nil)
	if err != nil {
		return nil, errors.New("could not create Telegram file request")
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, errors.New("could not download encrypted backup from Telegram")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Telegram file download failed with HTTP %d", resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, MaxEncryptedSize+1))
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 || len(payload) > MaxEncryptedSize {
		return nil, errors.New("downloaded encrypted backup has invalid size")
	}
	return payload, nil
}
