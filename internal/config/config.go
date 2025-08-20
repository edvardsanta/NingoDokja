package config

import (
	"log"

	"github.com/spf13/viper"
)

type BotConfig struct {
	Token                    string `mapstructure:"bot_token"`
	NewsChannelID            string `mapstructure:"news_channel_id"`
	OlympicChannelID         string `mapstructure:"olympic_channel_id"`
	OlympicChannelFinishedID string `mapstructure:"olympic_channel_finished_id"`
	OlympicChannelRunningID  string `mapstructure:"olympic_channel_running_id"`
	GuildID                  string `mapstructure:"guild_id"`
}

type RedisConfig struct {
	Addr string `mapstructure:"redis_addr"`
}

type Config struct {
	Bot         BotConfig   `mapstructure:"bot"`
	Redis       RedisConfig `mapstructure:"redis"`
	Environment string      `mapstructure:"environment"`
}

var AppConfig Config

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
	var redisConfig RedisConfig

	err := viper.Unmarshal(&botConfig)
	if err != nil {
		log.Fatalf("Não foi possível carregar a configuração do bot: %v", err)
	}

	err = viper.Unmarshal(&redisConfig)
	if err != nil {
		log.Fatalf("Não foi possível carregar a configuração do Redis: %v", err)
	}

	AppConfig = Config{
		Bot:   botConfig,
		Redis: redisConfig,
	}
	validateConfig()
}

func loadFromEnv() {
	envVars := []string{
		"bot_token", "news_channel_id", "olympic_channel_id",
		"olympic_channel_finished_id", "olympic_channel_running_id",
		"guild_id", "redis_addr", "postgres_host", "postgres_port",
		"postgres_user", "postgres_password", "postgres_db_name",
	}

	for _, envVar := range envVars {
		viper.BindEnv(envVar)
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
		"bot_token":       AppConfig.Bot.Token,
		"news_channel_id": AppConfig.Bot.NewsChannelID,
		"guild_id":        AppConfig.Bot.GuildID,
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
