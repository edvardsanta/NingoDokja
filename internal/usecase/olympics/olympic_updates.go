package olympics

import (
	"fmt"
	"read_books/internal/logger"
	olympyc_model "read_books/internal/scraper/model"
	scraper "read_books/internal/scraper/sites"
	"read_books/internal/utils"
	"strings"

	"github.com/bwmarrin/discordgo"
)

var sentGames = make(map[string]bool)

type GameInfo struct {
	RunningThreadID  string
	FinishedThreadID string
	MessageID        string
}

var gameMessages = make(map[string]*GameInfo)

func SendOlympicUpdates(session *discordgo.Session, runningChannelID string, finishedChannelID string) error {
	games, err := getOlympicInfo()
	if err != nil {
		return err
	}

	currentGames := filterCurrentGames(games)

	if len(currentGames) == 0 {
		session.ChannelMessageSend(runningChannelID, "Nenhum jogo está ocorrendo no momento.")
		return nil
	}

	for _, game := range currentGames {
		message := ""
		results := ""
		parsedStartDate := utils.ParseDateString(game.StartDate)
		parsedEndDate := utils.ParseDateString(game.EndDate)
		startDateStr := utils.FormatDate(parsedStartDate)
		endDateStr := utils.FormatDate(parsedEndDate)

		if game.Status == "FINISHED" {
			if gameInfo, exists := gameMessages[game.ID]; exists {
				competitors := game.Competitors
				if len(competitors) > 10 {
					results += fmt.Sprintf("__**10 primeiros competidores de %d:**__\n", len(competitors))
					competitors = competitors[:10]
				}
				for _, competitor := range competitors {
					flag := getFlagEmoji(competitor.NOC)
					results += fmt.Sprintf("%s %s: %s\n", flag, competitor.Name, competitor.Results.Mark)
				}
				header := fmt.Sprintf("__**Resultado final de %s (%s):**__", game.DisciplineName, game.EventUnitName)
				message = fmt.Sprintf("%s\n%s\nInício: %s\nTérmino: %s", header, results, startDateStr, endDateStr)

				if gameInfo.FinishedThreadID == "" {
					gameInfo.FinishedThreadID = finishedChannelID
				}

				_, err = session.ChannelMessageSend(gameInfo.FinishedThreadID, message)
				if err != nil {
					logger.Error("Erro ao enviar mensagem para tópico de jogos finalizados: %v", err)
					continue
				}

				err = session.ChannelMessageDelete(gameInfo.RunningThreadID, gameInfo.MessageID)
				if err != nil {
					logger.Error("Erro ao deletar mensagem do tópico de jogos em andamento: %v", err)
					continue
				}

				sentGames[game.ID] = true
			}
		} else if game.Status == "RUNNING" {
			message = fmt.Sprintf("Jogo em andamento: %s (%s)\nInício: %s\nTérmino previsto: %s", game.DisciplineName, game.EventUnitName, startDateStr, endDateStr)
			for _, competitor := range game.Competitors {
				flag := getFlagEmoji(competitor.NOC)
				results += fmt.Sprintf("%s ", flag)
			}
			message += fmt.Sprintf("\nCompetidores: %s", results)

			var threadID string
			if gameInfo, exists := gameMessages[game.ID]; exists {
				threadID = gameInfo.RunningThreadID
			} else {
				threadID = runningChannelID
				gameMessages[game.ID] = &GameInfo{RunningThreadID: threadID}
			}

			msg, err := session.ChannelMessageSend(threadID, message)
			if err != nil {
				logger.Error("Erro ao enviar mensagem para tópico de jogos em andamento: %v", err)
				continue
			}

			gameMessages[game.ID].MessageID = msg.ID
		}
	}
	return nil
}

func SendOlympicMedals(session *discordgo.Session, channelId string) {

}

func getOlympicInfo() ([]olympyc_model.Unit, error) {
	scraper := scraper.NewScraper("olympic")
	var response olympyc_model.Response
	err := scraper.GetResultsByJson(&response)
	if err != nil {
		logger.Error("Erro ao buscar por JSON", err)
		return response.Units, err
	}

	logger.Info("Informações dos Jogos Olímpicos obtidas com sucesso.")
	return response.Units, nil
}

func filterCurrentGames(games []olympyc_model.Unit) []olympyc_model.Unit {
	var currentGames []olympyc_model.Unit

	for _, game := range games {
		if game.Status == "RUNNING" || (game.Status == "FINISHED" && !sentGames[game.ID]) {
			currentGames = append(currentGames, game)
		}
	}

	return currentGames
}

func getFlagEmoji(noc string) string {
	// Conversão das letras do código NOC para o código Unicode das bandeiras
	// A é 0x1F1E6, B é 0x1F1E7, e assim por diante
	var flagBuilder strings.Builder
	for _, char := range noc {
		flagBuilder.WriteString(fmt.Sprintf("%c", 0x1F1E6+char-'A'))
	}
	return flagBuilder.String()
}
