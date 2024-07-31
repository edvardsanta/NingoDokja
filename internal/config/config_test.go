package config

import (
	"os"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func setupTestEnv() {
	os.Setenv("APP_BOT_TOKEN", "test_bot_token")
	os.Setenv("APP_NEWS_CHANNEL_ID", "test_news_channel_id")
	os.Setenv("APP_OLYMPIC_CHANNEL_ID", "test_olympic_channel_id")
	os.Setenv("APP_OLYMPIC_CHANNEL_FINISHED_ID", "test_olympic_channel_finished_id")
	os.Setenv("APP_OLYMPIC_CHANNEL_RUNNING_ID", "test_olympic_channel_running_id")
	os.Setenv("APP_GUILD_ID", "test_guild_id")
	os.Setenv("APP_REDIS_ADDR", "test_redis_addr")
	os.Setenv("APP_POSTGRES_HOST", "test_postgres_host")
	os.Setenv("APP_POSTGRES_PORT", "5432")
	os.Setenv("APP_POSTGRES_USER", "test_postgres_user")
	os.Setenv("APP_POSTGRES_PASSWORD", "test_postgres_password")
	os.Setenv("APP_POSTGRES_DB_NAME", "test_postgres_db")
}

func teardownTestEnv() {
	os.Unsetenv("APP_BOT_TOKEN")
	os.Unsetenv("APP_NEWS_CHANNEL_ID")
	os.Unsetenv("APP_OLYMPIC_CHANNEL_ID")
	os.Unsetenv("APP_OLYMPIC_CHANNEL_FINISHED_ID")
	os.Unsetenv("APP_OLYMPIC_CHANNEL_RUNNING_ID")
	os.Unsetenv("APP_GUILD_ID")
	os.Unsetenv("APP_REDIS_ADDR")
	os.Unsetenv("APP_POSTGRES_HOST")
	os.Unsetenv("APP_POSTGRES_PORT")
	os.Unsetenv("APP_POSTGRES_USER")
	os.Unsetenv("APP_POSTGRES_PASSWORD")
	os.Unsetenv("APP_POSTGRES_DB_NAME")
}

func TestLoadConfigFromEnv(t *testing.T) {
	setupTestEnv()
	defer teardownTestEnv()
	os.Setenv("APP_ENVIRONMENT", "production")
	defer os.Unsetenv("APP_ENVIRONMENT")
	LoadConfig()

	assert.Equal(t, "test_bot_token", AppConfig.Bot.Token)
	assert.Equal(t, "test_news_channel_id", AppConfig.Bot.NewsChannelID)
	assert.Equal(t, "test_olympic_channel_id", AppConfig.Bot.OlympicChannelID)
	assert.Equal(t, "test_olympic_channel_finished_id", AppConfig.Bot.OlympicChannelFinishedID)
	assert.Equal(t, "test_olympic_channel_running_id", AppConfig.Bot.OlympicChannelRunningID)
	assert.Equal(t, "test_guild_id", AppConfig.Bot.GuildID)
	assert.Equal(t, "test_redis_addr", AppConfig.Redis.Addr)
	assert.Equal(t, "test_postgres_host", AppConfig.Postgres.Host)
	assert.Equal(t, "5432", AppConfig.Postgres.Port)
	assert.Equal(t, "test_postgres_user", AppConfig.Postgres.User)
	assert.Equal(t, "test_postgres_password", AppConfig.Postgres.Password)
	assert.Equal(t, "test_postgres_db", AppConfig.Postgres.DBName)
}

func TestValidateConfig(t *testing.T) {
	setupTestEnv()
	defer teardownTestEnv()

	LoadConfig()

	assert.NotPanics(t, func() {
		validateConfig()
	})
}

func TestLoadConfigFromFileAndEnv(t *testing.T) {
	// Create a temporary configuration file
	configContent := `
bot_token: "file_bot_token"
news_channel_id: "file_news_channel_id"
olympic_channel_id: "file_olympic_channel_id"
olympic_channel_finished_id: "file_olympic_channel_finished_id"
olympic_channel_running_id: "file_olympic_channel_running_id"
guild_id: "file_guild_id"
redis_addr: "file_redis_addr"
postgres_host: "file_postgres_host"
postgres_port: "5432"
postgres_user: "file_postgres_user"
postgres_password: "file_postgres_password"
postgres_db_name: "file_postgres_db"
`

	configPath := "./test_config.yml"
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	assert.NoError(t, err)
	defer os.Remove(configPath)

	teardownTestEnv()

	viper.SetConfigFile(configPath)
	err = viper.ReadInConfig()
	assert.NoError(t, err)

	loadFromFileAndEnv("test_config", "yml", ".")
	LoadConfig()

	assert.Equal(t, "file_bot_token", AppConfig.Bot.Token)
	assert.Equal(t, "file_news_channel_id", AppConfig.Bot.NewsChannelID)
	assert.Equal(t, "file_olympic_channel_id", AppConfig.Bot.OlympicChannelID)
	assert.Equal(t, "file_olympic_channel_finished_id", AppConfig.Bot.OlympicChannelFinishedID)
	assert.Equal(t, "file_olympic_channel_running_id", AppConfig.Bot.OlympicChannelRunningID)
	assert.Equal(t, "file_guild_id", AppConfig.Bot.GuildID)
	assert.Equal(t, "file_redis_addr", AppConfig.Redis.Addr)
	assert.Equal(t, "file_postgres_host", AppConfig.Postgres.Host)
	assert.Equal(t, "5432", AppConfig.Postgres.Port)
	assert.Equal(t, "file_postgres_user", AppConfig.Postgres.User)
	assert.Equal(t, "file_postgres_password", AppConfig.Postgres.Password)
	assert.Equal(t, "file_postgres_db", AppConfig.Postgres.DBName)
}
