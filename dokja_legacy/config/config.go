package config

import (
	"encoding/json"
	"log"

	"github.com/spf13/viper"
)

type ChannelConfig struct {
	ID   string `mapstructure:"id"`
	Name string `mapstructure:"name"`
	Type string `mapstructure:"type"`
}

type BotConfig struct {
	Token    string          `mapstructure:"app_bot_token"`
	Channels []ChannelConfig `mapstructure:"channels"`
	GuildID  string          `mapstructure:"app_guild_id"`
}

type QueueConfig struct {
	Addr string `mapstructure:"app_queue_addr"`
}

type Config struct {
	Bot         BotConfig   `mapstructure:"bot"`
	QueueConfig QueueConfig `mapstructure:"queue"`
	Environment string      `mapstructure:"environment"`
}

var AppConfig Config

func init() {
	LoadConfig()
}

func LoadConfig() {
	viper.AutomaticEnv()
	viper.SetEnvPrefix("APP")

	// Carregar configurações de acordo com o ambiente
	environment := viper.GetString("ENVIRONMENT")
	log.Printf("ENVIRONMENT: %s", environment)
	if environment == "production" {
		loadFromEnv()
	} else {
		loadFromFileAndEnv(".env", "env", ".")
	}

	var botConfig BotConfig
	var queueConfig QueueConfig

	err := viper.Unmarshal(&botConfig)
	if err != nil {
		log.Fatalf("Não foi possível carregar a configuração do bot: %v", err)
	}

	err = viper.Unmarshal(&queueConfig)
	if err != nil {
		log.Fatalf("Não foi possível carregar a configuração do Redis: %v", err)
	}

	// Unmarshal APP_CHANNELS if present
	channelsEnv := viper.GetString("app_channels")
	if channelsEnv != "" {
		var channels []ChannelConfig
		err := json.Unmarshal([]byte(channelsEnv), &channels)
		if err != nil {
			log.Fatalf("Erro ao parsear APP_CHANNELS: %v", err)
		}
		botConfig.Channels = channels
	}

	AppConfig = Config{
		Bot:         botConfig,
		QueueConfig: queueConfig,
	}
	validateConfig()
}

func loadFromEnv() {
	envVars := []string{
		"bot_token", "guild_id", "queue_addr", "channels",
	}

	for _, envVar := range envVars {
		err := viper.BindEnv("APP_" + envVar)
		if err != nil {
			println(err.Error())
		}
	}
}

func loadFromFileAndEnv(configName, configType, configPath string) {
	viper.SetConfigName(configName)
	viper.SetConfigType(configType)
	viper.AddConfigPath(configPath)

	err := viper.ReadInConfig()
	if err != nil {
		log.Printf("Erro ao carregar o arquivo de configuração: %v", err)
	} else {
		log.Printf("Arquivo de configuração %s carregado com sucesso", configName)
	}

	loadFromEnv()
}

func validateConfig() {
	requiredFields := map[string]string{
		"bot_token":  AppConfig.Bot.Token,
		"guild_id":   AppConfig.Bot.GuildID,
		"queue_addr": AppConfig.QueueConfig.Addr,
		// "postgres_host":               AppConfig.Postgres.Host,
		// "postgres_port":               AppConfig.Postgres.Port,
		// "postgres_user":               AppConfig.Postgres.User,
		// "postgres_password":           AppConfig.Postgres.Password,
		// "postgres_db_name":            AppConfig.Postgres.DBName,
	}
	for key, value := range requiredFields {
		if value == "" {
			log.Fatalf("%s não está definido", key)
		}
	}
}
