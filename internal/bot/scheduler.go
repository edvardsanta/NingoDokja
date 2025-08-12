package bot

import (
	"fmt"
	"log"
	"time"

	"github.com/robfig/cron/v3"
)

func (bot *Bot) StartScheduler() {
	c := cron.New()
	var entryID cron.EntryID
	entryID, _ = c.AddFunc("@hourly", func() {
		sendMemes(bot.Session, bot.NewsChannelID)
		nextTime := c.Entry(entryID).Next
		log.Printf("Notícias enviadas. Próximo envio: %s", nextTime.Format("02-01-2006 15:04"))
	})
	c.Start()

	sendMemes(bot.Session, bot.NewsChannelID)

	nextTime := c.Entry(entryID).Next
	log.Printf(fmt.Sprintf("Memes enviados. Próximo envio: %s", nextTime.Format(time.RFC1123)))
}
