package config

import (
	"log"

	"github.com/spf13/viper"
)

var BotToken string
var NewsChannelID string
var GuildID string

func LoadConfig(configName string, configType string, configPath string) {
	viper.SetConfigName(configName)
	viper.SetConfigType(configType)
	viper.AddConfigPath(configPath)

	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("Erro ao carregar o arquivo de configuração: %v", err)
	}

	BotToken = viper.GetString("BOT_TOKEN")
	if BotToken == "" {
		log.Fatal("BOT_TOKEN não está definido no arquivo de configuração")
	}
	NewsChannelID = viper.GetString("NEWS_CHANNEL_ID")
	if NewsChannelID == "" {
		log.Fatal("NEWS_CHANNEL_ID não está definido no arquivo de configuração")
	}

	GuildID = viper.GetString("GUILD_ID")
	if GuildID == "" {
		log.Fatal("GUILD_ID não está definido no arquivo de configuração")
	}
}
