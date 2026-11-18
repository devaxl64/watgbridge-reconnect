package routeidentity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	waTypes "go.mau.fi/whatsmeow/types"
)

// Marker is stored in the Telegram group description. It contains an
// authenticated, encrypted WhatsApp JID—not a readable phone number.
const Marker = "WATGBRIDGE-V1:"
const maximumIDBytes = 256

func key() ([]byte, error) {
	encoded := os.Getenv("WATG_BACKUP_KEY")
	if encoded == "" {
		return nil, errors.New("WATG_BACKUP_KEY missing; cannot protect conversation identity")
	}
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(b) != 32 {
		return nil, errors.New("WATG_BACKUP_KEY must encode 32 bytes")
	}
	return b, nil
}

func CanonicalWhatsAppJID(raw string) (string, error) {
	jid, err := waTypes.ParseJID(strings.TrimSpace(raw))
	if err != nil || jid.User == "" ||
		(jid.Server != "g.us" && jid.Server != "s.whatsapp.net" && jid.Server != "lid") {
		return "", errors.New("not a supported WhatsApp contact/group identifier")
	}
	return jid.ToNonAD().String(), nil
}

func Encrypt(waChatID string) (string, error) {
	canonical, err := CanonicalWhatsAppJID(waChatID)
	if err != nil { return "", err }
	if len(canonical) > maximumIDBytes { return "", errors.New("WhatsApp ID too long") }
	k, err := key()
	if err != nil { return "", err }
	block, err := aes.NewCipher(k)
	if err != nil { return "", err }
	aead, err := cipher.NewGCM(block)
	if err != nil { return "", err }
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil { return "", err }
	sealed := aead.Seal(nil, nonce, []byte(canonical), []byte(Marker))
	encoded := base64.RawURLEncoding.EncodeToString(append(nonce, sealed...))
	if len(encoded)+len(Marker) > 255 {
		return "", fmt.Errorf("encrypted Telegram group identity exceeds description limit")
	}
	return Marker + encoded, nil
}

func Decrypt(description string) (string, bool, error) {
	if !strings.HasPrefix(description, Marker) {
		return "", false, nil
	}
	k, err := key()
	if err != nil { return "", true, err }
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(description, Marker))
	if err != nil { return "", true, errors.New("invalid encrypted group identity") }
	block, err := aes.NewCipher(k)
	if err != nil { return "", true, err }
	aead, err := cipher.NewGCM(block)
	if err != nil { return "", true, err }
	if len(raw) < aead.NonceSize()+aead.Overhead() {
		return "", true, errors.New("truncated encrypted group identity")
	}
	plaintext, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(Marker))
	if err != nil {
		return "", true, errors.New("cannot authenticate encrypted group identity")
	}
	id, err := CanonicalWhatsAppJID(string(plaintext))
	if err != nil { return "", true, err }
	return id, true, nil
}
