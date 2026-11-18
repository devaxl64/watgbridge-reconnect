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
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

var (
	ErrNotConfigured = errors.New("Telegram user client not configured")
	ErrNotAuthorized = errors.New("Telegram user client not yet authenticated; use /tglogin in bot private chat")
	ErrCodeNotRequested = errors.New("Telegram did not request a login code")
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

	loginRequests chan struct{}
	loginCodes chan string
	operations chan operation
	ready atomic.Bool
	waitingForCode atomic.Bool
	started atomic.Bool
}

func NewFromEnvironment(notify func(string)) (*Manager, error) {
	idStr := strings.TrimSpace(os.Getenv("WATG_TG_APP_ID"))
	hash := strings.TrimSpace(os.Getenv("WATG_TG_APP_HASH"))
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
		loginCodes:make(chan string,1),
		operations:make(chan operation),
	},nil
}

var global *Manager

// Register is called once by main before command handlers are registered.
func Register(m *Manager) { global = m }
func Active() *Manager { return global }
func (m *Manager) Ready() bool { return m != nil && m.ready.Load() }

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

func (m *Manager) SubmitCode(code string) error {
	if m == nil { return ErrNotConfigured }
	code = strings.TrimSpace(code)
	if len(code) < 4 || len(code) > 8 {
		return errors.New("invalid Telegram login code")
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' { return errors.New("Telegram login code contains non-numeric characters") }
	}
	if !m.waitingForCode.Load() {
		return ErrCodeNotRequested
	}
	select {
	case m.loginCodes <- code:
		return nil
	default:
		return errors.New("Telegram login code already supplied")
	}
}

func (m *Manager) Start(ctx context.Context) {
	if m == nil || !m.started.CompareAndSwap(false, true) { return }
	go m.loop(ctx)
}

func (m *Manager) loop(ctx context.Context) {
	defer m.ready.Store(false)
	for ctx.Err() == nil {
		client := telegram.NewClient(m.appID, m.appHash, telegram.Options{
			SessionStorage: &session.FileStorage{Path:m.sessionFile},
			NoUpdates:true, // group creation and lookups are request-only
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
				flow := auth.NewFlow(auth.Constant(m.phone,m.password,
					auth.CodeAuthenticatorFunc(func(codeCtx context.Context, _ *tg.AuthSentCode) (string,error) {
						m.waitingForCode.Store(true)
						defer m.waitingForCode.Store(false)
						m.notify("O Telegram enviou um código para sua conta. Envie /tgcodigo 12345 ao bot no privado.")
						select {
						case code := <-m.loginCodes: return code,nil
						case <-codeCtx.Done(): return "",codeCtx.Err()
						}
					})),auth.SendCodeOptions{})
				if err := client.Auth().IfNecessary(runCtx,flow); err != nil { return err }
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
			// Avoid exposing private credentials/session details in messages.
			m.notify("Conexão MTProto interrompida. Verifique os logs; farei nova tentativa.")
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
