package main

import (
	"os"
	"read_books/dokja_store/prompts/prompttest"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(prompttest.Run(m)) }
