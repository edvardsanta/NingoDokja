package bot

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

func (bot *Bot) StartScheduler() {
	// Configura o cron job para enviar notícias a cada hora
	c := cron.New()
	var entryID cron.EntryID
	entryID, _ = c.AddFunc("@hourly", func() {
		sendNews(bot.Session, bot.NewsChannelID)
		nextTime := c.Entry(entryID).Next
		bot.Session.ChannelMessageSend(bot.NewsChannelID, fmt.Sprintf("Enviando mais notícias daqui a uma hora. Próximo envio: %s", nextTime.Format(time.RFC1123)))
	})
	c.Start()

	// Envia notícias imediatamente ao iniciar
	sendNews(bot.Session, bot.NewsChannelID)

	// Mostrar o próximo horário de envio inicial
	nextTime := c.Entry(entryID).Next
	bot.Session.ChannelMessageSend(bot.NewsChannelID, fmt.Sprintf("Notícias enviadas. Próximo envio: %s", nextTime.Format(time.RFC1123)))
}
