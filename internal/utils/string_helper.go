package utils

import (
	"log"
	"time"
)

func ParseDateString(dateStr string) time.Time {
	parsedTime, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		log.Println("erro ao analisar a data: ", err)
		return time.Time{}
	}

	// Defina o local para Brasília
	location, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		log.Println("erro ao carregar a localização: ", err)
		return time.Time{}
	}

	// Converta o horário para o fuso horário de Brasília
	return parsedTime.In(location)
}

func FormatDate(t time.Time) string {
	return t.Format("02-01-2006 15:04")
}
