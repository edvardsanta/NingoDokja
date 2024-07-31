package config

import (
	"log"

	"github.com/spf13/viper"
)

var BotToken string
var NewsChannelID string
var OlympicChannelID string
var OlympicChannelFinishedID string
var OlympicChannelRunningID string
var GuildID string
var RedisAddr string

func LoadConfig() {
	// Configurar Viper para ler variáveis de ambiente
	viper.AutomaticEnv()
	viper.SetEnvPrefix("app") // Prefixo para variáveis de ambiente, exemplo: APP_BOT_TOKEN

	environment := viper.GetString("ENVIRONMENT")
	if environment == "production" {
		// Apenas variáveis de ambiente em produção
		loadFromEnv()
	} else {
		// Carregar de arquivo e variáveis de ambiente em desenvolvimento
		loadFromFileAndEnv(".env", "env", ".")
	}
}

func loadFromEnv() {
	viper.BindEnv("BOT_TOKEN")
	viper.BindEnv("NEWS_CHANNEL_ID")
	viper.BindEnv("OLYMPIC_CHANNEL_ID")
	viper.BindEnv("OLYMPIC_CHANNEL_FINISHED_ID")
	viper.BindEnv("OLYMPIC_CHANNEL_RUNNING_ID")
	viper.BindEnv("GUILD_ID")
	viper.BindEnv("REDIS_ADDR")
	BotToken = viper.GetString("BOT_TOKEN")
	if BotToken == "" {
		log.Fatal("BOT_TOKEN não está definido como variável de ambiente")
	}

	NewsChannelID = viper.GetString("NEWS_CHANNEL_ID")
	if NewsChannelID == "" {
		log.Fatal("NEWS_CHANNEL_ID não está definido como variável de ambiente")
	}

	OlympicChannelID = viper.GetString("OLYMPIC_CHANNEL_ID")
	if OlympicChannelID == "" {
		log.Fatal("OLYMPIC_CHANNEL_ID não está definido como variável de ambiente")
	}

	OlympicChannelFinishedID = viper.GetString("OLYMPIC_CHANNEL_FINISHED_ID")
	if OlympicChannelFinishedID == "" {
		log.Fatal("OLYMPIC_CHANNEL_FINISHED_ID não está definido como variável de ambiente")
	}

	OlympicChannelRunningID = viper.GetString("OLYMPIC_CHANNEL_RUNNING_ID")
	if OlympicChannelRunningID == "" {
		log.Fatal("OLYMPIC_CHANNEL_RUNNING_ID não está definido como variável de ambiente")
	}

	GuildID = viper.GetString("GUILD_ID")
	if GuildID == "" {
		log.Fatal("GUILD_ID não está definido como variável de ambiente")
	}

	RedisAddr = viper.GetString("REDIS_ADDR")
	if RedisAddr == "" {
		log.Fatal("REDIS_ADDR não está definido como variável de ambiente")
	}
}

func loadFromFileAndEnv(configName string, configType string, configPath string) {
	viper.SetConfigName(configName)
	viper.SetConfigType(configType)
	viper.AddConfigPath(configPath)

	err := viper.ReadInConfig()
	if err != nil {
		log.Printf("Erro ao carregar o arquivo de configuração: %v", err)
	}

	loadFromEnv()
}
