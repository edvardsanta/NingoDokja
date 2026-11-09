package ownerconfig

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// DefaultVisionsPath is where Ningo's convictions live by default. The file stays out of Git: the
// worldview belongs to the operator, not to the public repository.
const DefaultVisionsPath = ".dokja/visions.md"

const maxVisionsBytes = 8192

// LoadVisions reads Ningo's convictions as free text. An absent file means Ningo has no configured
// worldview. Error messages deliberately omit the file's content.
func LoadVisions(path string) (string, error) {
	if path == "" {
		path = DefaultVisionsPath
	}
	return loadPrivateText(path, "visions")
}

// loadPrivateText reads an operator's private text file of at most 8 KiB. An absent file is empty
// text. Error messages name the file's kind and never its content.
func loadPrivateText(path, kind string) (string, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("open %s file: %w", kind, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxVisionsBytes+1))
	if err != nil {
		return "", fmt.Errorf("read %s file: %w", kind, err)
	}
	if len(data) > maxVisionsBytes {
		return "", fmt.Errorf("%s file exceeds 8 KiB", kind)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("%s file must be UTF-8 text", kind)
	}
	return strings.TrimSpace(string(data)), nil
}
