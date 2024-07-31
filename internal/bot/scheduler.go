package bot

import (
	"fmt"
	"log"
	"time"

	"github.com/robfig/cron/v3"
)

func (bot *Bot) StartScheduler() {
	// Configura o cron job para enviar notícias a cada hora
	sendOlympicUpdates(bot.Session, bot.OlympicChannelRunningID, bot.OlympicChannelFinishedID)

	c := cron.New()
	var entryID cron.EntryID
	entryID, _ = c.AddFunc("@hourly", func() {
		sendNews(bot.Session, bot.NewsChannelID)
		nextTime := c.Entry(entryID).Next
		bot.Session.ChannelMessageSend(bot.NewsChannelID, fmt.Sprintf("Enviando mais notícias daqui a uma hora. Próximo envio: %s", nextTime.Format(time.RFC1123)))
		sendOlympicUpdates(bot.Session, bot.OlympicChannelRunningID, bot.OlympicChannelFinishedID)
		log.Printf("Notícias enviadas. Próximo envio: %s", nextTime.Format("02-01-2006 15:04"))
		log.Printf("Atualizações olímpicas enviadas. Próximo envio: %s", nextTime.Format("02-01-2006 15:04"))
	})
	c.Start()

	// Envia notícias imediatamente ao iniciar
	sendNews(bot.Session, bot.NewsChannelID)

	// Mostrar o próximo horário de envio inicial
	nextTime := c.Entry(entryID).Next
	bot.Session.ChannelMessageSend(bot.NewsChannelID, fmt.Sprintf("Notícias enviadas. Próximo envio: %s", nextTime.Format(time.RFC1123)))
}
