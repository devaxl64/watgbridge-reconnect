package groupclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tgerr"
	"github.com/skip2/go-qrcode"
	"github.com/gotd/td/tg"
)

var (
	ErrNotConfigured = errors.New("Telegram user client not configured")
	ErrNotAuthorized = errors.New("Telegram user client not yet authenticated; use /tglogin in bot private chat")
	ErrCodeNotRequested = errors.New("O Telegram bloqueia códigos compartilhados em conversas. Use /tglogin e autentique por QR Code.")
)

type operation struct {
	ctx context.Context
	fn func(context.Context, *tg.Client) error
	done chan error
}

// Manager maintains a Telegram USER MTProto session alongside the lightweight
// Telegram BOT API connection. Only the user session may create supergroups.
// A single MTProto worker serializes writes, limiting account rate-limit risk.
type Manager struct {
	appID int
	appHash string
	phone string
	password string
	sessionFile string
	notify func(string)
	qrNotify func([]byte) error

	loginRequests chan struct{}
	operations chan operation
	ready atomic.Bool
	// Once a full remote scan succeeds, repeated provisioning batches can use
	// the stable local index instead of re-reading every Telegram group.
	remoteCatalogReady atomic.Bool
	started atomic.Bool
}

func NewFromEnvironment(notify func(string)) (*Manager, error) {
	idStr := strings.TrimSpace(os.Getenv("WATG_TG_APP_ID"))
	hash := strings.TrimSpace(os.Getenv("WATG_TG_APP_HASH"))
	// Keep the existing Northflank API_ID / API_HASH values working.
	if idStr == "" { idStr = strings.TrimSpace(os.Getenv("API_ID")) }
	if hash == "" { hash = strings.TrimSpace(os.Getenv("API_HASH")) }
	phone := strings.TrimSpace(os.Getenv("WATG_TG_PHONE"))
	if idStr == "" && hash == "" && phone == "" { return nil, nil }
	if idStr == "" || hash == "" || phone == "" {
		return nil, errors.New("WATG_TG_APP_ID, WATG_TG_APP_HASH and WATG_TG_PHONE must all be configured")
	}
	appID, err := strconv.Atoi(idStr)
	if err != nil || appID <= 0 { return nil, errors.New("WATG_TG_APP_ID is invalid") }
	filename := os.Getenv("WATG_TG_SESSION_FILE")
	if filename == "" { filename = "telegram-user.json" }
	if notify == nil { notify = func(string){} }
	return &Manager{
		appID:appID, appHash:hash, phone:phone,
		password:os.Getenv("WATG_TG_PASSWORD"),
		sessionFile:filename, notify:notify,
		loginRequests:make(chan struct{},1),
		operations:make(chan operation),
	},nil
}

var global *Manager

// Register is called once by main before command handlers are registered.
func Register(m *Manager) { global = m }
func Active() *Manager { return global }
func (m *Manager) Ready() bool { return m != nil && m.ready.Load() }
// ForceRemoteRecovery makes the next Sync reconstruct Telegram routes again.
func (m *Manager) ForceRemoteRecovery() { if m != nil { m.remoteCatalogReady.Store(false) } }

func (m *Manager) BeginLogin() error {
	if m == nil { return ErrNotConfigured }
	if m.Ready() { return nil }
	select {
	case m.loginRequests <- struct{}{}:
		return nil
	default:
		return errors.New("Telegram login already requested")
	}
}

// SubmitCode is intentionally disabled. Sending any Telegram login code into
// a bot chat causes Telegram to invalidate it (even if we delete the message).
func (m *Manager) SubmitCode(_ string) error {
	if m == nil { return ErrNotConfigured }
	return ErrCodeNotRequested
}

// SetQRNotifier configures delivery of short-lived Telegram QR tokens as PNG
// photos in the owner's private chat. It must be set before Start.
func (m *Manager) SetQRNotifier(fn func([]byte) error) {
	if m != nil { m.qrNotify = fn }
}

// qrLogin never sends a numeric Telegram sign-in code to a chat or log.
func (m *Manager) qrLogin(ctx context.Context, client *telegram.Client, loggedIn qrlogin.LoggedIn) error {
	if m.qrNotify == nil {
		return errors.New("QR notifier not configured")
	}
	loginCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	_, err := client.QR().Auth(loginCtx, loggedIn, func(_ context.Context, token qrlogin.Token) error {
		png, err := qrcode.Encode(token.URL(), qrcode.High, 560)
		if err != nil { return errors.New("could not generate Telegram QR code") }
		if err := m.qrNotify(png); err != nil {
			return errors.New("could not send Telegram QR code to owner")
		}
		return nil
	})
	if err == nil { return nil }
	if tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
		if m.password == "" {
			return errors.New("Telegram 2FA password required; set WATG_TG_PASSWORD")
		}
		_, err = client.Auth().Password(ctx, m.password)
		return err
	}
	return err
}

func (m *Manager) Start(ctx context.Context) {
	if m == nil || !m.started.CompareAndSwap(false, true) { return }
	go m.loop(ctx)
}

func (m *Manager) loop(ctx context.Context) {
	defer m.ready.Store(false)
	for ctx.Err() == nil {
		// QR login relies on UpdateLoginToken, so MTProto updates must be enabled.
		dispatcher := tg.NewUpdateDispatcher()
		loggedIn := qrlogin.OnLoginToken(&dispatcher)
		client := telegram.NewClient(m.appID, m.appHash, telegram.Options{
			SessionStorage: &session.FileStorage{Path:m.sessionFile},
			UpdateHandler: dispatcher,
		})
		err := client.Run(ctx, func(runCtx context.Context) error {
			status, err := client.Auth().Status(runCtx)
			if err != nil { return err }
			if !status.Authorized {
				m.notify("Sessão da conta Telegram ainda não vinculada. Use /tglogin no privado do bot.")
				select {
				case <-runCtx.Done(): return runCtx.Err()
				case <-m.loginRequests:
				}
				if err := m.qrLogin(runCtx, client, loggedIn); err != nil { return err }
			}
			m.ready.Store(true)
			defer m.ready.Store(false)
			m.notify("Conta Telegram autenticada. Agora é possível recuperar e criar grupos individuais.")
			for {
				select {
				case <-runCtx.Done(): return runCtx.Err()
				case job := <-m.operations:
					if err := job.ctx.Err(); err != nil {
						job.done <- err
					} else {
						job.done <- job.fn(job.ctx,client.API())
					}
				}
			}
		})
		if ctx.Err()!=nil { return }
		if err!=nil {
			// Do not leak Telegram login tokens or credential values to chat.
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				m.notify("QR Code expirou. Use /tglogin para gerar outro.")
			case strings.Contains(err.Error(),"2FA password required"):
				m.notify("O Telegram exige senha de duas etapas. Configure WATG_TG_PASSWORD no Northflank e reinicie para tentar novamente.")
			default:
				m.notify("Não foi possível autenticar por QR. Confira os logs e use /tglogin para tentar novamente.")
			}
		}
		select {
		case <-ctx.Done():return
		case <-time.After(30*time.Second):
		}
	}
}

func (m *Manager) Do(ctx context.Context, fn func(context.Context,*tg.Client)error) error {
	if m==nil {return ErrNotConfigured}
	if !m.Ready() {return ErrNotAuthorized}
	job:=operation{ctx:ctx,fn:fn,done:make(chan error,1)}
	select {
	case m.operations<-job:
	case <-ctx.Done():return ctx.Err()
	}
	select {
	case err:=<-job.done:return err
	case <-ctx.Done():return fmt.Errorf("Telegram user request canceled: %w",ctx.Err())
	}
}
