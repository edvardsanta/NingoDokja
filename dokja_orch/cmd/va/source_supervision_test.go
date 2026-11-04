package main

import (
	"strings"
	"testing"
)

func TestSourceSupervisionStartupIsExplicitAndSanitized(t *testing.T) {
	if value, err := openSourceJournal("", ""); err != nil || value != nil {
		t.Fatal("disabled feature attempted journal access")
	}
	for _, tc := range []struct{ token, dsn string }{{"", "private-dsn"}, {"CHANGE_ME_" + strings.Repeat("a", 40), "private-dsn"}, {strings.Repeat("a", 40), ""}, {strings.Repeat("a", 40), "host='invalid private-connection"}} {
		_, err := openSourceJournal(tc.token, tc.dsn)
		if err == nil {
			t.Fatal("invalid config accepted")
		}
		if strings.Contains(err.Error(), "private-") {
			t.Fatal("native connection details leaked")
		}
	}
}

func TestSourceSupervisionRejectsSharedServiceCredential(t *testing.T) {
	token := strings.Repeat("s", 40)
	if _, err := openSourceJournal(token, "private-dsn", token); err == nil {
		t.Fatal("service credential granted operator scope")
	}
}
