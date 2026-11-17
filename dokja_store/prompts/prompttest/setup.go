// Package prompttest provides synthetic wiring fixtures, never production prompts.
package prompttest

import (
	"fmt"
	"os"
	"path/filepath"
	"read_books/dokja_store/prompts"
	"strings"
	"testing"
)

var Installed bool

func Run(m *testing.M) int {
	if _, err := os.Stat(prompts.Directory()); err == nil {
		if err := prompts.Validate(prompts.Names...); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		Installed = true
	}
	if Installed {
		prompts.SetDirectory(prompts.Directory())
		defer prompts.SetDirectory("")
		return m.Run()
	}
	directory, err := os.MkdirTemp("", "dokja-prompt-fixtures-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(directory)
	for _, name := range prompts.Names {
		content := fmt.Sprintf("fixture:%s %s", name, formats[name])
		if name == "chat_persona_owneridentitynote1" {
			content = "fixture:" + name + " owner_configured=%t; current_speaker_is_owner=%t; owner_mentioned=%t"
		}
		if err := os.WriteFile(filepath.Join(directory, name+".md"), []byte(strings.TrimSpace(content)), 0600); err != nil {
			panic(err)
		}
	}
	prompts.SetDirectory(directory)
	defer prompts.SetDirectory("")
	return m.Run()
}

// RequireInstalled marks tests of operator-authored wording, which is private
// configuration rather than a policy shipped by the repository.
func RequireInstalled(t *testing.T) {
	t.Helper()
	if !Installed {
		t.Skip("operator prompt wording requires private .dokja/prompts files")
	}
}
