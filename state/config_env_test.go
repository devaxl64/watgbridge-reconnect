package state

import (
	"path/filepath"
	"testing"
)

func TestEnvironmentOnlyConfigWithoutYAML(t *testing.T) {
	t.Setenv("BOT_TOKEN", "fake-bot-token-for-test")
	t.Setenv("OWNER_ID", "8256480955")
	t.Setenv("TARGET_CHAT_ID", "-1003987753765")
	t.Setenv("WATG_SEPARATE_CHATS", "1")
	cfg := &Config{Path: filepath.Join(t.TempDir(), "missing-config.yaml")}
	cfg.SetDefaults()
	if err := cfg.LoadConfig(); err != nil { t.Fatal(err) }
	if cfg.Telegram.BotToken != "fake-bot-token-for-test" ||
		cfg.Telegram.OwnerID != 8256480955 ||
		cfg.Telegram.TargetChatID != -1003987753765 ||
		!cfg.Telegram.SeparateChats {
		t.Fatalf("incorrect env configuration: %+v", cfg.Telegram)
	}
	if cfg.Database["type"] != "sqlite" || cfg.Database["path"] != "./gobot.sqlite.db" {
		t.Fatalf("missing SQLite defaults: %+v", cfg.Database)
	}
}

func TestExistingYAMLConfigurationIsNotOverriddenByEnvironment(t *testing.T) {
	t.Setenv("BOT_TOKEN", "environment-token")
	t.Setenv("OWNER_ID", "765")
	t.Setenv("TARGET_CHAT_ID", "-100555")
	t.Setenv("WATG_SEPARATE_CHATS", "1")
	cfg := &Config{}
	cfg.SetDefaults()
	cfg.Telegram.BotToken = "yaml-token"
	cfg.Telegram.OwnerID = 123
	cfg.Telegram.TargetChatID = -100123
	cfg.Database = map[string]string{"type":"sqlite","path":"local.db"}
	if err := cfg.ApplyRuntimeEnvironment(); err != nil { t.Fatal(err) }
	if cfg.Telegram.BotToken != "yaml-token" || cfg.Telegram.OwnerID != 123 ||
		cfg.Telegram.TargetChatID != -100123 || cfg.Database["path"] != "local.db" {
		t.Fatal("original YAML values were unexpectedly overwritten")
	}
}

func TestEnvironmentOnlyRequiresOwnerID(t *testing.T) {
	t.Setenv("BOT_TOKEN", "fake-token")
	t.Setenv("OWNER_ID", "")
	t.Setenv("WATG_SEPARATE_CHATS", "0")
	cfg := &Config{}
	cfg.SetDefaults()
	if err := cfg.ApplyRuntimeEnvironment(); err == nil {
		t.Fatal("missing owner ID must be rejected before startup")
	}
}
