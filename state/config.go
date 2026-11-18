package state

import (
	"fmt"
	"errors"
	"strconv"
	"strings"
	"io"
	"net/url"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Path             string `yaml:"-"`
	TimeZone         string `yaml:"time_zone"`
	TimeFormat       string `yaml:"time_format"`
	GitExecutable    string `yaml:"git_executable"`
	GoExecutable     string `yaml:"go_executable"`
	FfmpegExecutable string `yaml:"ffmpeg_executable"`
	DebugMode        bool   `yaml:"debug_mode"`

	UseGithHubBinaries bool   `yaml:"use_github_binaries"`
	Architecture       string `yaml:"architecture"`

	Telegram struct {
		BotToken            string  `yaml:"bot_token"`
		APIURL              string  `yaml:"api_url"`
		SudoUsersID         []int64 `yaml:"sudo_users_id"`
		OwnerID             int64   `yaml:"owner_id"`
		TargetChatID        int64   `yaml:"target_chat_id"`
		SeparateChats       bool    `yaml:"separate_chats"`
		SelfHostedAPI       bool    `yaml:"self_hosted_api"`
		SendImagesAsFile    bool    `yaml:"send_images_as_file"`
		SendStickersAsFile   bool    `yaml:"send_stickers_as_file"`
		SkipVideoStickers   bool    `yaml:"skip_video_stickers"`
		SkipSettingCommands bool    `yaml:"skip_setting_commands"`
		SendMyPresence      bool    `yaml:"send_my_presence"`
		SendMyReadReceipts  bool    `yaml:"send_my_read_receipts"`
		SilentConfirmation  bool    `yaml:"silent_confirmation"`
		ConfirmationType    string  `yaml:"confirmation_type"`
		EmojiConfirmation   *bool   `yaml:"emoji_confirmation"`
		SkipStartupMessage  bool    `yaml:"skip_startup_message"`
		SpoilerViewOnce     bool    `yaml:"spoiler_as_viewonce"`
		Reactions           bool    `yaml:"reactions"`
		TagAllEnabled       bool    `yaml:"tag_all_enabled"`
		AutoReactWhenAllRead bool   `yaml:"auto_react_when_all_read"`
		AutoReactRemoveAfter int64  `yaml:"auto_react_remove_after_seconds"`
	} `yaml:"telegram"`

	WhatsApp struct {
		LoginDatabase struct {
			Type string `yaml:"type"`
			URL  string `yaml:"url"`
		} `yaml:"login_database"`
		StickerMetadata struct {
			PackName   string `yaml:"pack_name"`
			AuthorName string `yaml:"author_name"`
		} `yaml:"sticker_metadata"`
		SessionName                    string   `yaml:"session_name"`
		ClientMode                     string   `yaml:"client_mode"`
		TagAllAllowedGroups            []string `yaml:"tag_all_allowed_groups"`
		IgnoreChats                    []string `yaml:"ignore_chats"`
		StatusIgnoredChats             []string `yaml:"status_ignored_chats"`
		SkipDocuments                  bool     `yaml:"skip_documents"`
		SkipImages                     bool     `yaml:"skip_images"`
		SkipGIFs                       bool     `yaml:"skip_gifs"`
		SkipVideos                     bool     `yaml:"skip_videos"`
		SkipVoiceNotes                 bool     `yaml:"skip_voice_notes"`
		SkipAudios                     bool     `yaml:"skip_audios"`
		SkipStatus                     bool     `yaml:"skip_status"`
		SkipStickers                   bool     `yaml:"skip_stickers"`
		SkipContacts                   bool     `yaml:"skip_contacts"`
		SkipLocations                  bool     `yaml:"skip_locations"`
		SkipProfilePictureUpdates      bool     `yaml:"skip_profile_picture_updates"`
		SkipGroupSettingsUpdates       bool     `yaml:"skip_group_settings_updates"`
		SkipChatDetails                bool     `yaml:"skip_chat_details"`
		SendRevokedMessageUpdates      bool     `yaml:"send_revoked_message_updates"`
		SendEditedMessageUpdates       bool     `yaml:"send_edited_message_updates"`
		WhatsmeowDebugMode             bool     `yaml:"whatsmeow_debug_mode"`
		SendMyMessagesFromOtherDevices bool     `yaml:"send_my_messages_from_other_devices"`
		CreateThreadForInfoUpdates     bool     `yaml:"create_thread_for_info_updates"`
		SkipPinnedMessages             bool     `yaml:"skip_pinned_messages"`
		StatusMessageDurationSeconds   uint32   `yaml:"status_message_duration_seconds"`
		SkipOfflineBacklog             bool     `yaml:"skip_offline_backlog"`
	} `yaml:"whatsapp"`

	Database map[string]string `yaml:"database"`

	Backup struct {
		Mode         string `yaml:"mode"`
		CronSchedule string `yaml:"cron_schedule"`
		ThreadName   string `yaml:"thread_name"`
	} `yaml:"backup"`
}

func (cfg *Config) LoadConfig() error {
	configFilePath := cfg.Path

	if _, err := os.Stat(configFilePath); err != nil {
		// A container deployed from a registry image may have only runtime
		// environment variables, with no mounted YAML config file.
		if errors.Is(err, os.ErrNotExist) && os.Getenv("BOT_TOKEN") != "" {
			return cfg.ApplyRuntimeEnvironment()
		}
		return fmt.Errorf("error with config file path : %s", err)
	}

	configFile, err := os.Open(configFilePath)
	if err != nil {
		return fmt.Errorf("could not open config file : %s", err)
	}
	defer configFile.Close()

	configBody, err := io.ReadAll(configFile)
	if err != nil {
		return fmt.Errorf("could not read config file : %s", err)
	}

	err = yaml.Unmarshal(configBody, cfg)
	if err != nil {
		return fmt.Errorf("could not parse config file : %s", err)
	}

	whatsappLoginDB := cfg.WhatsApp.LoginDatabase
	if whatsappLoginDB.Type == "sqlite3" {
		parsedUrl, err := url.Parse(whatsappLoginDB.URL)
		if err != nil {
			return fmt.Errorf("whatsapp Login Database URL is not a valid URL")
		}
		if _, found := parsedUrl.Query()["_foreign_keys"]; !found {
			q := parsedUrl.Query()
			q.Set("_foreign_keys", "on")
			if q.Has("foreign_keys") {
				q.Del("foreign_keys")
			}
			parsedUrl.RawQuery = q.Encode()
			cfg.WhatsApp.LoginDatabase.URL = parsedUrl.String()
		}
	}

	return cfg.ApplyRuntimeEnvironment()
}

// ApplyRuntimeEnvironment is compatible with both the original YAML-based
// deployment and existing Northflank environment-only services.
// It fills missing fields, never overwrites values already in config.yaml.
func (cfg *Config) ApplyRuntimeEnvironment() error {
	if cfg.Telegram.BotToken == "" {
		cfg.Telegram.BotToken = strings.TrimSpace(os.Getenv("BOT_TOKEN"))
	}
	if cfg.Telegram.OwnerID == 0 {
		if owner := strings.TrimSpace(os.Getenv("OWNER_ID")); owner != "" {
			id, err := strconv.ParseInt(owner, 10, 64)
			if err != nil || id <= 0 { return errors.New("OWNER_ID must be a positive Telegram user ID") }
			cfg.Telegram.OwnerID = id
		}
	}
	if cfg.Telegram.TargetChatID == 0 {
		if target := strings.TrimSpace(os.Getenv("TARGET_CHAT_ID")); target != "" {
			id, err := strconv.ParseInt(target, 10, 64)
			if err != nil || id == 0 { return errors.New("TARGET_CHAT_ID must be a Telegram chat ID") }
			cfg.Telegram.TargetChatID = id
		}
	}
	if os.Getenv("WATG_SEPARATE_CHATS") == "1" { cfg.Telegram.SeparateChats = true }
	if cfg.Database == nil { cfg.Database = map[string]string{} }
	if cfg.Database["type"] == "" { cfg.Database["type"] = "sqlite" }
	if cfg.Database["type"] == "sqlite" && cfg.Database["path"] == "" {
		cfg.Database["path"] = "./gobot.sqlite.db"
	}
	if cfg.Telegram.BotToken == "" {
		return errors.New("Telegram bot token missing: set BOT_TOKEN or telegram.bot_token")
	}
	if cfg.Telegram.OwnerID <= 0 {
		return errors.New("Telegram owner missing: set OWNER_ID or telegram.owner_id")
	}
	if cfg.Telegram.SeparateChats && cfg.Telegram.TargetChatID == 0 {
		return errors.New("TARGET_CHAT_ID or telegram.target_chat_id is needed for legacy compatibility")
	}
	if len(cfg.Telegram.SudoUsersID) == 0 {
		cfg.Telegram.SudoUsersID = []int64{cfg.Telegram.OwnerID}
	}
	return nil
}

func (cfg *Config) SaveConfig() error {
	configFilePath := cfg.Path

	configFile, err := os.Create(configFilePath)
	if err != nil {
		return fmt.Errorf("could not open config file : %s", err)
	}
	defer configFile.Close()

	newConfigBody, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config into string : %s", err)
	}

	_, err = configFile.Write(newConfigBody)
	if err != nil {
		return fmt.Errorf("failed to write config file : %s", err)
	}

	return nil
}

func (cfg *Config) SetDefaults() {
	cfg.TimeZone = "UTC"

	cfg.WhatsApp.SessionName = "watgbridge"
	cfg.WhatsApp.ClientMode = "android"
	cfg.WhatsApp.LoginDatabase.Type = "sqlite3"
	cfg.WhatsApp.LoginDatabase.URL = "file:wawebstore.db?foreign_keys=on"
	cfg.WhatsApp.StickerMetadata.PackName = "WaTgBridge"
	cfg.WhatsApp.StickerMetadata.AuthorName = "WaTgBridge"
	cfg.WhatsApp.StatusMessageDurationSeconds = 86400

	cfg.Telegram.ConfirmationType = "emoji"

	cfg.Backup.Mode = "none"
	cfg.Backup.CronSchedule = "0 0 * * *"
	cfg.Backup.ThreadName = "Database Backups"
}
