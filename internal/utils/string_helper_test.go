package utils

import (
	"bytes"
	"log"
	"read_books/internal/logger"
	"testing"
	"time"
)

func init() {
	logger.Init()
}
func TestParseDateString(t *testing.T) {
	validDateStr := "2023-07-27T15:04:05Z"
	expectedTime := time.Date(2023, 7, 27, 15, 4, 5, 0, time.UTC)
	parsedTime := ParseDateString(validDateStr)
	if !parsedTime.Equal(expectedTime) {
		t.Errorf("Expected %v, got %v", expectedTime, parsedTime)
	}

	var buf bytes.Buffer
	log.SetOutput(&buf)
	invalidDateStr := "invalid-date"
	parsedTime = ParseDateString(invalidDateStr)
	if !parsedTime.IsZero() {
		t.Errorf("Expected zero time for invalid date string, got %v", parsedTime)
	}

	output := buf.String()
	expectedLog := "[ERROR] erro ao analisar a data: "
	if !contains(output, expectedLog) {
		t.Errorf("Expected log to contain '%s', got '%s'", expectedLog, output)
	}
}

func TestFormatDate(t *testing.T) {
	testTime := time.Date(2023, 7, 27, 15, 4, 5, 0, time.UTC)
	expectedFormat := "27-07-2023 15:04"
	formattedDate := FormatDate(testTime)
	if formattedDate != expectedFormat {
		t.Errorf("Expected %s, got %s", expectedFormat, formattedDate)
	}
}

func contains(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}
