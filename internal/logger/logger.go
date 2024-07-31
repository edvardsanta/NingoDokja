package logger

import (
	"log"
)

func Init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func Info(msg string) {
	log.Println("[INFO]", msg)
}

func Error(msg string, err error) {
	log.Printf("[ERROR] %s: %v\n", msg, err)
}
