package logger

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"testing"
)

func TestInit(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)

	Init()

	log.Print("test message")
	if !strings.Contains(buf.String(), "test message") {
		t.Errorf("Expected 'test message' in log output, got '%s'", buf.String())
	}
}

func TestInfo(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)

	Info("test info message")
	output := buf.String()
	if !strings.Contains(output, "[INFO] test info message") {
		t.Errorf("Expected '[INFO] test info message' in log output, got '%s'", output)
	}
}

func TestError(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)

	var testErr error = fmt.Errorf("test error")
	Error("test error message", testErr)
	output := buf.String()
	if !strings.Contains(output, "[ERROR] test error message: test error") {
		t.Errorf("Expected '[ERROR] test error message: test error' in log output, got '%s'", output)
	}
}
