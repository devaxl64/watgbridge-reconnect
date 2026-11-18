// Package sessionbackup keeps WhatsApp and bridge state in an encrypted
// Telegram backup, without ever storing credentials in a Git repository.
package sessionbackup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	Magic = "WATG-BK1"
	// Telegram Bot API getFile is limited to 20 MB: stay below it.
	MaxEncryptedSize = 18 * 1024 * 1024
	MaxPlainFileSize = 12 * 1024 * 1024
)

var expectedFiles = []string{"wawebstore.db", "gobot.sqlite.db"}
// The MTProto user session is small and optional: a bridge can be backed up
// before the owner's Telegram account has completed its first sign-in.
const optionalSession = "telegram-user.json"

// KeyFromEnvironment accepts exactly 32 random bytes in standard base64.
// This variable must be a Northflank runtime secret, never a repository file.
func KeyFromEnvironment() ([]byte, error) {
	encoded := os.Getenv("WATG_BACKUP_KEY")
	if encoded == "" {
		return nil, errors.New("WATG_BACKUP_KEY is not configured")
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, errors.New("WATG_BACKUP_KEY must be base64 encoding of exactly 32 random bytes")
	}
	return key, nil
}

// Seal encrypts a gzip-compressed, allowlisted tar archive using AES-256-GCM.
// Includes no secret metadata or unencrypted WhatsApp file contents.
func Seal(key []byte, files map[string][]byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("AES-256 key must have 32 bytes")
	}
	var plain bytes.Buffer
	zipWriter := gzip.NewWriter(&plain)
	tarWriter := tar.NewWriter(zipWriter)
	for _, name := range append(append([]string{}, expectedFiles...), optionalSession) {
		data, ok := files[name]
		if name == optionalSession && (!ok || len(data) == 0) {
			continue
		}
		if !ok || len(data) == 0 {
			return nil, fmt.Errorf("missing or empty database snapshot: %s", name)
		}
		if len(data) > MaxPlainFileSize {
			return nil, fmt.Errorf("snapshot too large: %s", name)
		}
		if err := tarWriter.WriteHeader(&tar.Header{
			Name: name, Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg,
		}); err != nil {
			return nil, err
		}
		if _, err := tarWriter.Write(data); err != nil {
			return nil, err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, err
	}
	if err := zipWriter.Close(); err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	result := append([]byte(Magic), nonce...)
	result = aead.Seal(result, nonce, plain.Bytes(), []byte(Magic))
	if len(result) > MaxEncryptedSize {
		return nil, errors.New("encrypted snapshot exceeds Telegram 18 MiB backup limit")
	}
	return result, nil
}

// Open refuses corrupt, tampered, unknown-format or path-traversal archives.
func Open(key, blob []byte) (map[string][]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("AES-256 key must have 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	minLen := len(Magic) + aead.NonceSize() + aead.Overhead()
	if len(blob) < minLen || len(blob) > MaxEncryptedSize || string(blob[:len(Magic)]) != Magic {
		return nil, errors.New("invalid encrypted session backup")
	}
	nonce := blob[len(Magic) : len(Magic)+aead.NonceSize()]
	ciphertext := blob[len(Magic)+aead.NonceSize():]
	data, err := aead.Open(nil, nonce, ciphertext, []byte(Magic))
	if err != nil {
		return nil, errors.New("session backup authentication failed: wrong key or damaged backup")
	}
	unzip, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer unzip.Close()
	tarReader := tar.NewReader(unzip)
	files := make(map[string][]byte, len(expectedFiles))
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			return nil, errors.New("unexpected archive entry type")
		}
		if header.Name != "wawebstore.db" && header.Name != "gobot.sqlite.db" && header.Name != optionalSession {
			return nil, errors.New("unknown archive entry name")
		}
		if _, duplicate := files[header.Name]; duplicate || header.Size <= 0 || header.Size > MaxPlainFileSize {
			return nil, errors.New("duplicate, empty or oversized archive entry")
		}
		content, err := io.ReadAll(io.LimitReader(tarReader, MaxPlainFileSize+1))
		if err != nil {
			return nil, err
		}
		if int64(len(content)) != header.Size {
			return nil, errors.New("archive entry size mismatch")
		}
		files[header.Name] = content
	}
	for _, name := range expectedFiles {
		if len(files[name]) == 0 {
			return nil, fmt.Errorf("archive missing required file %s", name)
		}
	}
	return files, nil
}

// RestoreMissing never replaces an existing database. The caller must do this
// before either SQLite database is opened. Files are written 0600 atomically.
func RestoreMissing(files map[string][]byte, paths map[string]string) (int, error) {
	restored := 0
	for _, name := range append(append([]string{}, expectedFiles...), optionalSession) {
		path := paths[name]
		data := files[name]
		if name == optionalSession && len(data) == 0 { continue }
		if path == "" || len(data) == 0 {
			return restored, fmt.Errorf("missing path or data for %s", name)
		}
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return restored, err
		}
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return restored, err
		}
		tmp, err := os.CreateTemp(dir, ".watg-restore-*.db")
		if err != nil {
			return restored, err
		}
		tmpName := tmp.Name()
		defer os.Remove(tmpName)
		if err := tmp.Chmod(0600); err != nil {
			tmp.Close()
			return restored, err
		}
		if _, err := tmp.Write(data); err != nil {
			tmp.Close()
			return restored, err
		}
		if err := tmp.Sync(); err != nil {
			tmp.Close()
			return restored, err
		}
		if err := tmp.Close(); err != nil {
			return restored, err
		}
		// Recheck prevents overwriting another process's file. This bridge runs
		// one instance; deployments must not use the same temporary path twice.
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.Rename(tmpName, path); err != nil {
			return restored, err
		}
		restored++
	}
	return restored, nil
}
