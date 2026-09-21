package cli

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// bomMark is the UTF-8 byte order mark some editors put at the start of a file.
var bomMark = string(rune(0xFEFF))

func writeFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadKnowledgeFileTakesTheTitleFromTheFirstHeading(t *testing.T) {
	path := writeFile(t, "Notas do Estoicismo.md", []byte(bomMark+"# Estoicismo prático\n\nA virtude basta."))

	document, err := ReadKnowledgeFile(path, "", "note", []string{"filosofia"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if document.Title != "Estoicismo prático" {
		t.Fatalf("expected the heading as title, got %q", document.Title)
	}
	if document.SourceID != "file:Notas-do-Estoicismo.md" {
		t.Fatalf("source id must come from the file name with only accepted characters, got %q", document.SourceID)
	}
	if strings.HasPrefix(document.Body, bomMark) {
		t.Fatal("the byte order mark must be dropped")
	}
	want := map[string]any{
		"title": "Estoicismo prático", "body": document.Body, "kind": "note",
		"source_id": "file:Notas-do-Estoicismo.md", "tags": []string{"filosofia"},
	}
	if got := document.Payload(); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected payload %#v", got)
	}
}

func TestReadKnowledgeFileTitleFallbacks(t *testing.T) {
	plain := writeFile(t, "ideias.txt", []byte("sem cabeçalho aqui"))
	document, err := ReadKnowledgeFile(plain, "", "note", nil)
	if err != nil || document.Title != "ideias" {
		t.Fatalf("expected the file name as title, got %q (%v)", document.Title, err)
	}
	document, err = ReadKnowledgeFile(plain, "  Meu título ", "note", nil)
	if err != nil || document.Title != "Meu título" {
		t.Fatalf("expected the explicit title, got %q (%v)", document.Title, err)
	}
	// A heading buried after prose is not the document's title.
	buried := writeFile(t, "b.md", []byte("prosa primeiro\n\n# Depois"))
	if document, _ := ReadKnowledgeFile(buried, "", "note", nil); document.Title != "b" {
		t.Fatalf("expected the file name, got %q", document.Title)
	}
}

func TestReadKnowledgeFileRejectsWhatItCannotStore(t *testing.T) {
	cases := map[string][]byte{
		"a.zip":   []byte("PK"),
		"run.exe": []byte("MZ"),
		"b.md":    {0xff, 0xfe, 0x00, 0x41},
		"c.txt":   []byte("   \n\n"),
		"d.txt":   []byte("tem\x00nulo"),
		"big.txt": []byte(strings.Repeat("a", maxKnowledgeFileBytes+1)),
		"e.pdf":   {},
	}
	for name, content := range cases {
		if _, err := ReadKnowledgeFile(writeFile(t, name, content), "", "note", nil); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
	if _, err := ReadKnowledgeFile(filepath.Join(t.TempDir(), "missing.md"), "", "note", nil); err == nil {
		t.Fatal("expected a missing file error")
	}
}

func TestBinaryFormatsAreSentAsUploadsForTheServiceToExtract(t *testing.T) {
	raw := []byte("%PDF-1.4 not really parsed here")
	document, err := ReadKnowledgeFile(writeFile(t, "Rate paper.PDF", raw), "", "paper", []string{"macro"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	payload := document.Payload()
	decoded, _ := base64.StdEncoding.DecodeString(payload["content_b64"].(string))
	if string(decoded) != string(raw) || payload["filename"] != "Rate paper.PDF" {
		t.Fatalf("the file must travel as it is, got %#v", payload)
	}
	if _, has := payload["body"]; has {
		t.Fatal("an upload must not also carry a body")
	}
	if _, has := payload["title"]; has {
		t.Fatal("without --title the service uses the document's own title")
	}
	if payload["source_id"] != "file:Rate-paper.PDF" || payload["kind"] != "paper" {
		t.Fatalf("unexpected payload %#v", payload)
	}

	titled, _ := ReadKnowledgeFile(writeFile(t, "a.docx", []byte("zip")), " Memo ", "note", nil)
	if titled.Payload()["title"] != "Memo" {
		t.Fatalf("an explicit title must be sent, got %#v", titled.Payload())
	}
	for _, extension := range []string{".epub", ".html", ".htm", ".xml", ".rss", ".atom"} {
		if _, err := ReadKnowledgeFile(writeFile(t, "x"+extension, []byte("data")), "", "note", nil); err != nil {
			t.Fatalf("%s should be accepted: %v", extension, err)
		}
	}
}

func TestIsKnowledgeSourceTellsAddressesAndSchemesFromFiles(t *testing.T) {
	existing := writeFile(t, "odd:name.md", []byte("x"))
	cases := map[string]bool{
		"https://docs.example/a": true,
		"HTTP://docs.example/a":  true,
		"inbox:2024/note":        true,
		"notes.md":               false,
		"./notes.md":             false,
		"dir/notes.md":           false,
		"C":                      false,
		"-":                      false,
		existing:                 false, // an existing file wins over the scheme shape
	}
	for arg, want := range cases {
		if got := IsKnowledgeSource(arg); got != want {
			t.Errorf("IsKnowledgeSource(%q) = %v, want %v", arg, got, want)
		}
	}
}

func TestASourceIsSentForTheServiceToFetch(t *testing.T) {
	payload := NewKnowledgeSource("  https://docs.example/a ", " Note ", "article", []string{"macro"}).Payload()
	want := map[string]any{"source": "https://docs.example/a", "title": "Note", "kind": "article", "tags": []string{"macro"}}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("unexpected payload %#v", payload)
	}
}

func TestReadKnowledgeStdinNeedsATitle(t *testing.T) {
	if _, err := ReadKnowledgeStdin(strings.NewReader("texto"), " ", "note", nil); err == nil {
		t.Fatal("expected a title error")
	}
	document, err := ReadKnowledgeStdin(strings.NewReader("uma ideia solta"), "Ideia", "note", nil)
	if err != nil || document.Body != "uma ideia solta" || document.SourceID != "" {
		t.Fatalf("unexpected document %#v (%v)", document, err)
	}
	if _, ok := document.Payload()["source_id"]; ok {
		t.Fatal("a note from stdin lets the service derive its source id")
	}
}

func TestBuildKnowledgeSearchPayload(t *testing.T) {
	payload, err := buildKnowledgeSearchPayload("  o que é virtude? ", 3)
	if err != nil || payload["query"] != "o que é virtude?" || payload["k"] != 3 {
		t.Fatalf("unexpected payload %#v (%v)", payload, err)
	}
	for _, k := range []int{0, 21} {
		if _, err := buildKnowledgeSearchPayload("x", k); err == nil {
			t.Fatalf("expected k=%d to be rejected", k)
		}
	}
	if _, err := buildKnowledgeSearchPayload("   ", 5); err == nil {
		t.Fatal("expected an empty question to be rejected")
	}
}

func TestKnowledgeResultUnwrapsBothResponseModes(t *testing.T) {
	// VA_RESPONSE_MODE=debug nests the answer under the domain name.
	answer, skipped, err := knowledgeResult(map[string]any{
		"result": map[string]any{"knowledge": map[string]any{"total": 2.0}},
	})
	if err != nil || skipped != "" || answer["total"] != 2.0 {
		t.Fatalf("verbose: unexpected %#v %q %v", answer, skipped, err)
	}

	// The default compact mode puts the answer directly in "result".
	answer, skipped, err = knowledgeResult(map[string]any{
		"event_id": "e1", "workflow": "knowledge", "domain": "knowledge",
		"result": map[string]any{"total": 2.0},
	})
	if err != nil || skipped != "" || answer["total"] != 2.0 {
		t.Fatalf("compact: unexpected %#v %q %v", answer, skipped, err)
	}

	_, skipped, err = knowledgeResult(map[string]any{
		"result": map[string]any{"skipped": true, "reason": "service knowledge is disabled"},
	})
	if err != nil || skipped != "service knowledge is disabled" {
		t.Fatalf("expected the skip reason, got %q (%v)", skipped, err)
	}

	for _, bad := range []any{"x", map[string]any{}, map[string]any{"result": "text"}} {
		if _, _, err := knowledgeResult(bad); err == nil {
			t.Fatalf("expected an error for %#v", bad)
		}
	}
}

func TestFormatIngestSaysWhatHappened(t *testing.T) {
	cases := []struct {
		answer map[string]any
		want   string
	}{
		{map[string]any{"source_id": "file:a.md", "created": true, "changed": true, "chunks": 4.0, "degraded": false}, "added"},
		{map[string]any{"source_id": "file:a.md", "created": false, "changed": true, "chunks": 4.0, "degraded": false}, "updated"},
		{map[string]any{"source_id": "file:a.md", "created": false, "changed": false}, "unchanged"},
		{map[string]any{"source_id": "file:a.md", "created": true, "changed": true, "chunks": 4.0, "degraded": true, "reason": "unreachable"}, "knowledge reindex"},
		{map[string]any{"documents": []any{map[string]any{}, map[string]any{}}, "count": 2.0, "created": 1.0, "unchanged": 1.0, "chunks": 5.0, "degraded": false}, "2 entries (1 new, 1 unchanged)"},
	}
	for _, tc := range cases {
		if got := FormatIngest("a.md", tc.answer); !strings.Contains(got, tc.want) {
			t.Fatalf("expected %q in %q", tc.want, got)
		}
	}
}

func TestFormatSearchMarksRelevanceAndWarnsWhenNothingIs(t *testing.T) {
	hit := func(rank float64, relevant bool, score any) any {
		return map[string]any{"rank": rank, "relevant": relevant, "score": score, "title": "Kant", "heading": "Razão",
			"source_id": "note:kant", "text": "  muito   texto\nem várias linhas "}
	}
	out := FormatSearch(map[string]any{"relevant_count": 1.0, "hits": []any{hit(1, true, 0.61), hit(2, false, 0.31)}})
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[0], "* 1. [0.61] Kant › Razão (note:kant)") || !strings.HasPrefix(lines[2], "  2. [0.31]") {
		t.Fatalf("unexpected rendering:\n%s", out)
	}
	if !strings.Contains(out, "muito texto em várias linhas") {
		t.Fatalf("whitespace must be collapsed:\n%s", out)
	}

	weak := FormatSearch(map[string]any{"relevant_count": 0.0, "hits": []any{hit(1, false, 0.3)}})
	if !strings.Contains(weak, "no passage looks relevant") {
		t.Fatalf("expected a no-relevance warning:\n%s", weak)
	}

	degraded := FormatSearch(map[string]any{"degraded": true, "reason": "embedding server unreachable", "hits": []any{hit(1, true, nil)}})
	if !strings.Contains(degraded, "! degraded: embedding server unreachable") || !strings.Contains(degraded, "[  -  ]") {
		t.Fatalf("expected the degraded notice and a blank score:\n%s", degraded)
	}
	if got := FormatSearch(map[string]any{"hits": []any{}}); got != "nothing found\n" {
		t.Fatalf("unexpected empty rendering %q", got)
	}
}

func TestFormatListShowsPartialEmbeddings(t *testing.T) {
	out := FormatList(map[string]any{"total": 2.0, "documents": []any{
		map[string]any{"source_id": "note:a", "kind": "note", "chunks": 3.0, "embedded": 3.0, "title": "A"},
		map[string]any{"source_id": "note:b", "kind": "note", "chunks": 3.0, "embedded": 1.0, "title": "B"},
	}})
	if strings.Contains(strings.Split(out, "\n")[0], "embedded") || !strings.Contains(out, "(1/3 embedded)") {
		t.Fatalf("unexpected rendering:\n%s", out)
	}
	if FormatList(map[string]any{}) != "no documents stored\n" {
		t.Fatal("unexpected empty rendering")
	}
}

func TestKnowledgeIsASwitchableService(t *testing.T) {
	if _, err := buildServicePayload("knowledge", false); err != nil {
		t.Fatalf("knowledge must be a known service: %v", err)
	}
}

func TestFormatSearchDoesNotRepeatTheTitleInTheHeadingPath(t *testing.T) {
	out := FormatSearch(map[string]any{"relevant_count": 1.0, "hits": []any{map[string]any{
		"rank": 1.0, "relevant": true, "score": 0.5, "title": "Estoicismo", "heading": "Estoicismo > Virtude",
		"source_id": "file:e.md", "text": "x",
	}}})
	if !strings.Contains(out, "Estoicismo › Virtude (file:e.md)") || strings.Contains(out, "Estoicismo › Estoicismo") {
		t.Fatalf("unexpected rendering:\n%s", out)
	}
}
