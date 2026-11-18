package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"watgbridge/database"
	"watgbridge/modules"
	"watgbridge/groupclient"
	"watgbridge/sessionbackup"
	"watgbridge/state"
	"watgbridge/telegram"
	"watgbridge/utils"
	"watgbridge/whatsapp"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/go-co-op/gocron"
	"go.uber.org/zap"
)

func findExecutablePath(logger *zap.Logger, cfg *state.Config, name string, fatalOnErr bool) string {
	path, err := exec.LookPath(name)
	if err != nil && !errors.Is(err, exec.ErrDot) {
		if fatalOnErr {
			logger.Fatal(fmt.Sprintf("failed to set %s executable path", name), zap.Error(err))
		}
	}
	return path
}

func sendRestartNotification(logger *zap.Logger) bool {
	isRestarted, found := os.LookupEnv("WATG_IS_RESTARTED")
	if !found || isRestarted != "1" {
		return false
	}

	chatIdString, chatIdFound := os.LookupEnv("WATG_CHAT_ID")
	msgIdString, msgIdFound := os.LookupEnv("WATG_MESSAGE_ID")
	if !chatIdFound || !msgIdFound {
		return false
	}

	chatId, err1 := strconv.ParseInt(chatIdString, 10, 64)
	msgId, err2 := strconv.ParseInt(msgIdString, 10, 64)
	if err1 != nil || err2 != nil {
		return false
	}

	opts := gotgbot.SendMessageOpts{
		ReplyParameters: &gotgbot.ReplyParameters{
			MessageId: msgId,
		},
	}

	_, err := state.State.TelegramBot.SendMessage(chatId, "Successfully restarted", &opts)
	if err != nil {
		logger.Error("failed to send restart notification", zap.Error(err))
		return false
	}
	return true
}

func main() {
	// Load configuration file
	cfg := state.State.Config
	cfg.SetDefaults()

	if len(os.Args) > 1 {
		cfg.Path = os.Args[1]
	}

	err := cfg.LoadConfig()
	if err != nil {
		panic(fmt.Errorf("failed to load config file: %s", err))
	}

	deprecatedOptions := state.GetDeprecatedConfigOptions(cfg)
	if deprecatedOptions != nil {
		fmt.Println("The following options have been deprecated/removed:")
		for num, opt := range deprecatedOptions {
			fmt.Printf("%d. %s: %s\n", num+1, opt.Name, opt.Description)
		}
	}

	if cfg.Telegram.APIURL == "" {
		cfg.Telegram.APIURL = gotgbot.DefaultAPIURL
	}

	if cfg.DebugMode {
		developmentConfig := zap.NewDevelopmentConfig()
		developmentConfig.OutputPaths = append(developmentConfig.OutputPaths, "debug.log")
		state.State.Logger, err = developmentConfig.Build()
		if err != nil {
			panic(fmt.Errorf("failed to initialize development logger: %s", err))
		}
		state.State.Logger = state.State.Logger.Named("WaTgBridge_Dev")
	} else {
		productionConfig := zap.NewProductionConfig()
		state.State.Logger, err = productionConfig.Build()
		if err != nil {
			panic(fmt.Errorf("failed to initialize production logger: %s", err))
		}
		state.State.Logger = state.State.Logger.Named("WaTgBridge")
	}
	logger := state.State.Logger

	logger.Debug("loaded config file and started logger",
		zap.String("config_path", cfg.Path),
		zap.Bool("development_mode", cfg.DebugMode),
	)
	_ = logger.Sync()

	// Create local location for time
	if cfg.TimeZone == "" {
		cfg.TimeZone = "UTC"
	}
	locLoc, err := time.LoadLocation(cfg.TimeZone)
	if err != nil {
		logger.Fatal("failed to set time zone",
			zap.String("time_zone", cfg.TimeZone),
			zap.Error(err),
		)
	}
	state.State.LocalLocation = locLoc

	if cfg.WhatsApp.SessionName == "" {
		cfg.WhatsApp.SessionName = "watgbridge"
	}

	if cfg.WhatsApp.LoginDatabase.Type == "" || cfg.WhatsApp.LoginDatabase.URL == "" {
		cfg.WhatsApp.LoginDatabase.Type = "sqlite3"
		cfg.WhatsApp.LoginDatabase.URL = "file:wawebstore.db?foreign_keys=on"
		logger.Debug("using sqlite3 as WhatsApp login database")
		_ = logger.Sync()
	}

	configChanged := false
	if cfg.GitExecutable == "" {
		cfg.GitExecutable = findExecutablePath(logger, cfg, "git", true)
		logger.Info("setting path to git executable", zap.String("path", cfg.GitExecutable))
		configChanged = true
	}

	if cfg.GoExecutable == "" {
		cfg.GoExecutable = findExecutablePath(logger, cfg, "go", true)
		logger.Info("setting path to go executable", zap.String("path", cfg.GoExecutable))
		configChanged = true
	}

	if cfg.FfmpegExecutable == "" && !cfg.Telegram.SkipVideoStickers {
		cfg.FfmpegExecutable = findExecutablePath(logger, cfg, "ffmpeg", true)
		logger.Info("setting path to ffmpeg executable", zap.String("path", cfg.FfmpegExecutable))
		configChanged = true
	}

	if configChanged {
		_ = logger.Sync()
		if err = cfg.SaveConfig(); err != nil {
			logger.Fatal("failed to save config file", zap.Error(err))
		}
	}

	// Restore an encrypted Telegram backup BEFORE either SQLite DB is opened.
	// This is opt-in; deployments without WATG_BACKUP_KEY behave as before.
	backupManager, err := sessionbackup.NewManagerFromEnv(
		cfg.Telegram.BotToken, cfg.Telegram.OwnerID,
		cfg.WhatsApp.LoginDatabase.Type, cfg.WhatsApp.LoginDatabase.URL,
		cfg.Database["type"], cfg.Database["path"],
	)
	if err != nil {
		logger.Fatal("invalid encrypted backup configuration", zap.Error(err))
	}
	var backupRestoreFailed bool
	if backupManager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		restored, restoreErr := backupManager.RestoreIfMissing(ctx)
		cancel()
		if restoreErr != nil {
			backupRestoreFailed = true
			logger.Error("could not restore encrypted Telegram session backup; no session will be deleted",
				zap.Error(restoreErr))
		} else if restored {
			logger.Info("restored missing SQLite database files from encrypted Telegram backup")
		}
	}

	// Setup database
	db, err := database.Connect()
	if err != nil {
		logger.Fatal("could not connect to database",
			zap.Error(err),
		)
	}
	state.State.Database = db
	err = database.AutoMigrate()
	if err != nil {
		logger.Fatal("could not migrate database tabels",
			zap.Error(err),
		)
	}

	err = telegram.NewTelegramClient()
	if err != nil {
		logger.Fatal("failed to initialize telegram client",
			zap.Error(err),
		)
	}
	_ = logger.Sync()

	// A Telegram USER session (MTProto) is needed to create true separate
	// supergroups. Credentials are runtime secrets, never GitHub files.
	// The encrypted Telegram backup was already restored above.
	userClient, err := groupclient.NewFromEnvironment(func(message string) {
		_, _ = state.State.TelegramBot.SendMessage(
			cfg.Telegram.OwnerID, message, &gotgbot.SendMessageOpts{})
	})
	if err != nil {
		logger.Fatal("invalid Telegram user configuration", zap.Error(err))
	}
	groupclient.Register(userClient)
	if userClient != nil {
		userClient.Start(context.Background())
	}

	// When encrypted backups are enabled, keep the bot online even when the
	// WhatsApp session was lost. The owner can use /codigo from one phone.
	deferPairing := backupManager != nil || os.Getenv("WATG_DEFER_PAIRING") == "1"
	err = whatsapp.NewWhatsAppClientWithPairing(deferPairing)
	if err != nil {
		logger.Fatal("failed to initialize whatsapp client",
			zap.Error(err),
		)
	}
	_ = logger.Sync()

	state.State.StartTime = time.Now().UTC()
	// Periodically re-discover all Telegram groups from their encrypted
	// descriptions. No new groups by default until the owner authorizes it.
	if cfg.Telegram.SeparateChats && userClient != nil {
		// Separate-chat mode gradually creates all missing groups, one per sync cycle.
		// Set WATG_AUTOCREATE_BATCH=0 to pause creating new groups.
		autoCreate := 1
		if raw := os.Getenv("WATG_AUTOCREATE_BATCH"); raw != "" {
			count, parseErr := strconv.Atoi(raw)
			if parseErr == nil && count >= 0 && count <= 5 {
				autoCreate = count
			}
		}
		go func() {
			// Allow WhatsApp history-sync metadata to arrive first.
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for {
				<-ticker.C
				wa := state.State.WhatsAppClient
				if wa == nil || !wa.IsLoggedIn() || !userClient.Ready() {continue}
				ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
				report, syncErr := userClient.Sync(ctx, autoCreate,
					state.State.TelegramBot.Username)
				cancel()
				if syncErr != nil {
					logger.Warn("Telegram group synchronization deferred", zap.Error(syncErr))
				} else if report.Created > 0 || report.Recovered > 0 {
					logger.Info("Telegram groups synchronized",
						zap.Int("recovered", report.Recovered),
						zap.Int("created", report.Created))
				}
			}
		}()
	}


	// Snapshot only authenticated sessions; never overwrite a good backup
	// with a newly created but unpaired device database.
	if backupManager != nil {
		backupManager.RunPeriodicBackup(context.Background(), 30*time.Minute,
			func() bool {
				waClient := state.State.WhatsAppClient
				return waClient != nil && waClient.Store != nil &&
					waClient.Store.ID != nil && waClient.IsLoggedIn()
			},
			func(backupErr error) {
				logger.Error("encrypted Telegram session backup failed", zap.Error(backupErr))
			})
	}

	s := gocron.NewScheduler(time.UTC)
	s.TagsUnique()
	_, _ = s.Every(1).Hour().Tag("foo").Do(func() {
		contacts, err := state.State.WhatsAppClient.Store.Contacts.GetAllContacts(context.Background())
		if err == nil {
			_ = database.ContactNameBulkAddOrUpdate(contacts)
		}
	})

	telegram.AddTelegramHandlers()
	modules.LoadModuleHandlers()

	if !cfg.Telegram.SkipSettingCommands {
		err = utils.TgRegisterBotCommands(state.State.TelegramBot, state.State.TelegramCommands...)
		if err != nil {
			logger.Error("failed to set my commands",
				zap.Error(err),
			)
		}
	} else {
		err = utils.TgRegisterBotCommands(state.State.TelegramBot)
		if err != nil {
			logger.Error("failed to set my commands to empty",
				zap.Error(err),
			)
		}
	}
	_ = logger.Sync()

	startMessageSuccessful := sendRestartNotification(logger)

	if !startMessageSuccessful && !cfg.Telegram.SkipStartupMessage {
		message := "Successfully started WaTgBridge"
		if state.State.WhatsAppClient.Store.ID == nil {
			message = "WATGBridge iniciado, mas o WhatsApp está sem sessão. " +
				"Use /codigo 55DDDNÚMERO na conversa privada para conectar pelo celular."
		} else if backupRestoreFailed {
			message += "\\nAviso: não foi possível recuperar o backup criptografado do Telegram. Confira os logs."
		}
		state.State.TelegramBot.SendMessage(cfg.Telegram.OwnerID, message, &gotgbot.SendMessageOpts{})
	}

	utils.StartAutomaticDatabaseBackups()

	state.State.TelegramUpdater.Idle()
}
