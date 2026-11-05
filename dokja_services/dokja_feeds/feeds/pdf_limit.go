package feeds

import (
	"os"
	"strconv"
	"strings"
)

func pdfLimitBytes() int64 {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("DOKJA_PDF_MAX_MIB")))
	if err != nil || n < 1 || n > 100 {
		n = 25
	}
	return int64(n) << 20
}
