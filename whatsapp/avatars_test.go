package whatsapp

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDownloadAvatarPreviewRejectsHttpOrHugeImage(t *testing.T) {
	if _, err := DownloadAvatarPreview(context.Background(), "http://localhost/avatar"); err == nil {
		t.Fatal("insecure URL accepted")
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(bytes.Repeat([]byte{'x'}, maxAvatarBytes+1))
	}))
	defer server.Close()
	old := avatarHTTPClient
	avatarHTTPClient = &http.Client{
		Timeout: time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify:true}}, // test-only self-signed server
	}
	defer func() { avatarHTTPClient = old }()
	if _, err := DownloadAvatarPreview(context.Background(), server.URL); err == nil {
		t.Fatal("oversized avatar accepted")
	}
	_ = errors.New("")
}
