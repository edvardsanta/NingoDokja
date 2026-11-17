package runtime

import (
	"os"
	"read_books/dokja_store/prompts"
	"read_books/dokja_store/prompts/prompttest"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(prompttest.Run(m)) }

func transcriptFixture(t *testing.T) string {
	t.Helper()
	text, err := prompts.Read("runtime_chat_transcript_instructions")
	if err != nil {
		t.Fatal(err)
	}
	return text
}
