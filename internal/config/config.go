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

type PostgresConfig struct {
	Host     string `mapstructure:"postgres_host"`
	Port     string `mapstructure:"postgres_port"`
	User     string `mapstructure:"postgres_user"`
	Password string `mapstructure:"postgres_password"`
	DBName   string `mapstructure:"postgres_db_name"`
}

type Config struct {
	Bot         BotConfig      `mapstructure:"bot"`
	Redis       RedisConfig    `mapstructure:"redis"`
	Postgres    PostgresConfig `mapstructure:"postgres"`
	Environment string         `mapstructure:"environment"`
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
	var postgresConfig PostgresConfig

	err := viper.Unmarshal(&botConfig)
	if err != nil {
		log.Fatalf("Não foi possível carregar a configuração do bot: %v", err)
	}

	err = viper.Unmarshal(&redisConfig)
	if err != nil {
		log.Fatalf("Não foi possível carregar a configuração do Redis: %v", err)
	}

	err = viper.Unmarshal(&postgresConfig)
	if err != nil {
		log.Fatalf("Não foi possível carregar a configuração do PostgreSQL: %v", err)
	}
	// Atribuir valores para a configuração global
	AppConfig = Config{
		Bot:      botConfig,
		Redis:    redisConfig,
		Postgres: postgresConfig,
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
		"bot_token":                   AppConfig.Bot.Token,
		"news_channel_id":             AppConfig.Bot.NewsChannelID,
		"olympic_channel_id":          AppConfig.Bot.OlympicChannelID,
		"olympic_channel_finished_id": AppConfig.Bot.OlympicChannelFinishedID,
		"olympic_channel_running_id":  AppConfig.Bot.OlympicChannelRunningID,
		"guild_id":                    AppConfig.Bot.GuildID,
		"redis_addr":                  AppConfig.Redis.Addr,
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
