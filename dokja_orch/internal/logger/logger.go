package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

type Appender interface {
	Info(msg string)
	Error(msg string, err error)
	ErrorPrintf(format string, args ...interface{})
}

type ConsoleAppender struct{}

func (c *ConsoleAppender) Info(msg string) {
	log.Println("[INFO]", msg)
}

func (c *ConsoleAppender) Error(msg string, err error) {
	log.Printf("[ERROR] %s: %v\n", msg, err)
}

func (c *ConsoleAppender) ErrorPrintf(format string, args ...interface{}) {
	log.Printf("[ERROR] "+format+"\n", args...)
}

type FileAppender struct {
	mu   sync.Mutex
	file io.Writer
}

func NewFileAppender(path string) (*FileAppender, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &FileAppender{file: f}, nil
}

func (fa *FileAppender) Info(msg string) {
	fa.mu.Lock()
	defer fa.mu.Unlock()
	fmt.Fprintf(fa.file, "[INFO] %s\n", msg)
}

func (fa *FileAppender) Error(msg string, err error) {
	fa.mu.Lock()
	defer fa.mu.Unlock()
	fmt.Fprintf(fa.file, "[ERROR] %s: %v\n", msg, err)
}

func (fa *FileAppender) ErrorPrintf(format string, args ...interface{}) {
	fa.mu.Lock()
	defer fa.mu.Unlock()
	fmt.Fprintf(fa.file, "[ERROR] "+format+"\n", args...)
}

// Placeholder for future TelemetryAppender
// type TelemetryAppender struct { /* ... */ }
// func (t *TelemetryAppender) Info(msg string) { /* ... */ }
// func (t *TelemetryAppender) Error(msg string, err error) { /* ... */ }

var (
	appenders []Appender
	mu        sync.Mutex
)

// Init initializes the logger with default (console) appender.
func Init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	InitWithOptions(true, "")
}

func InitWithOptions(enableConsole bool, filePath string) {
	if log.Flags() == 0 {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}
	mu.Lock()
	defer mu.Unlock()
	appenders = nil
	if enableConsole {
		appenders = append(appenders, &ConsoleAppender{})
	}
	if filePath != "" {
		fa, err := NewFileAppender(filePath)
		if err == nil {
			appenders = append(appenders, fa)
		} else {
			log.Printf("[LOGGER] Failed to initialize file appender: %v\n", err)
		}
	}
}

func Info(msg string) {
	mu.Lock()
	apps := append([]Appender(nil), appenders...)
	mu.Unlock()
	for _, a := range apps {
		a.Info(msg)
	}
}

func Error(msg string, err error) error {
	mu.Lock()
	apps := append([]Appender(nil), appenders...)
	mu.Unlock()
	for _, a := range apps {
		a.Error(msg, err)
	}

	return err
}

func ErrorPrintf(format string, args ...interface{}) {
	mu.Lock()
	apps := append([]Appender(nil), appenders...)
	mu.Unlock()
	for _, a := range apps {
		a.ErrorPrintf(format, args...)
	}
}
