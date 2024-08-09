package bot

import (
	"log"
	"read_books/internal/usecase/audio"
	"read_books/internal/usecase/news"
	"read_books/internal/usecase/olympics"

	"github.com/bwmarrin/discordgo"
)

func sendNews(s *discordgo.Session, channelID string) {
	news.SendNews(s, channelID)
}

func sendOlympicUpdates(session *discordgo.Session, runningChannelID string, finishedChannelID string) {
	olympics.SendOlympicUpdates(session, runningChannelID, finishedChannelID)
}

func sendOlympicMedals(session *discordgo.Session, channelID string) {
	olympics.SendOlympicMedals(session, channelID)
}

func joinChannel(s *discordgo.Session, guild *discordgo.Guild, userId string, playSounds bool) (*discordgo.VoiceConnection, error) {
	var vc *discordgo.VoiceConnection
	var err error
	for _, vs := range guild.VoiceStates {
		if vs.UserID == userId {
			vc, err = s.ChannelVoiceJoin(guild.ID, vs.ChannelID, false, false)
			if err != nil {
				return nil, err
			}

			if playSounds {
				go func() {
					if err := audio.PlayAllSounds(vc); err != nil {
						log.Printf("Error playing sound: %v", err)
					}
				}()
			}
			break
		}
	}

	return vc, nil
}
