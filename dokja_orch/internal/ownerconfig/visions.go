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
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("open visions file: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxVisionsBytes+1))
	if err != nil {
		return "", fmt.Errorf("read visions file: %w", err)
	}
	if len(data) > maxVisionsBytes {
		return "", fmt.Errorf("visions file exceeds 8 KiB")
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("visions file must be UTF-8 text")
	}
	return strings.TrimSpace(string(data)), nil
}
