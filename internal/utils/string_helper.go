package utils

import (
	"read_books/internal/logger"
	"time"
)

func ParseDateString(dateStr string) time.Time {
	parsedTime, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		logger.Error("erro ao analisar a data: ", err)
		return time.Time{}
	}
	return parsedTime
}

func FormatDate(t time.Time) string {
	return t.Format("02-01-2006 15:04")
}
