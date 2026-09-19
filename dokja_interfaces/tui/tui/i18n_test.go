package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// The existing tests assert the Portuguese text, so they run in Portuguese. Tests of the
// English default switch the language and restore it.
func TestMain(m *testing.M) {
	SetLanguage("pt")
	os.Exit(m.Run())
}

func withLanguage(t *testing.T, tag string) {
	t.Helper()
	previous := Language()
	SetLanguage(tag)
	t.Cleanup(func() { SetLanguage(previous) })
}

// usedMessages returns every literal passed as the first argument of tr(...) in the
// package sources.
func usedMessages(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	used := map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); !ok || ident.Name != "tr" {
				return true
			}
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				used[text] = true
			}
			return true
		})
	}
	return used
}

func TestEveryMessageUsedInTheCodeHasAPortugueseTranslation(t *testing.T) {
	var missing []string
	for message := range usedMessages(t) {
		if _, ok := ptMessages[message]; !ok {
			missing = append(missing, message)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("messages without a Portuguese translation:\n  %s", strings.Join(missing, "\n  "))
	}
}

func TestTheCatalogHoldsNoStaleOrIdenticalEntries(t *testing.T) {
	used := usedMessages(t)
	for message, translated := range ptMessages {
		if !used[message] {
			t.Errorf("stale catalog entry, not used in the code: %q", message)
		}
		if message == translated {
			t.Errorf("entry %q is identical in both languages; drop it", message)
		}
	}
}

var verbs = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)

func TestTranslationsKeepTheSameFormatVerbsInTheSameOrder(t *testing.T) {
	for message, translated := range ptMessages {
		if got, want := verbs.FindAllString(translated, -1), verbs.FindAllString(message, -1); strings.Join(got, "") != strings.Join(want, "") {
			t.Errorf("format verbs differ between %q (%v) and %q (%v)", message, want, translated, got)
		}
		if strings.HasSuffix(message, "\n") != strings.HasSuffix(translated, "\n") {
			t.Errorf("trailing newline differs for %q", message)
		}
	}
}

func TestLanguageSelection(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(name string) string { return values[name] }
	}
	cases := []struct {
		values map[string]string
		want   string
	}{
		{map[string]string{}, "en"},
		{map[string]string{"LANG": "pt_BR.UTF-8"}, "pt"},
		{map[string]string{"LANG": "en_US.UTF-8"}, "en"},
		{map[string]string{"LANG": "pt_BR.UTF-8", "DOKJA_LANG": "en"}, "en"},
		{map[string]string{"LANG": "en_US", "DOKJA_LANG": "pt-BR"}, "pt"},
		{map[string]string{"LC_ALL": "pt_PT", "LANG": "en_US"}, "pt"},
		{map[string]string{"LANG": "C"}, "en"},
		{map[string]string{"LANG": "POSIX", "LC_MESSAGES": "pt_BR"}, "pt"},
		{map[string]string{"DOKJA_LANG": "klingon"}, "en"},
		{map[string]string{"LANG": "pt"}, "pt"},
	}
	for _, tc := range cases {
		if got := DetectLanguage(env(tc.values)); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.values, got, tc.want)
		}
	}
	withLanguage(t, "PT_br")
	if Language() != "pt" {
		t.Fatalf("expected pt, got %q", Language())
	}
	SetLanguage("de")
	if Language() != "en" {
		t.Fatalf("an unknown language must fall back to English, got %q", Language())
	}
}

func TestMessagesFallBackToEnglishAndFormatArguments(t *testing.T) {
	withLanguage(t, "pt")
	if got := tr("no such message %d", 3); got != "no such message 3" {
		t.Fatalf("expected the English text with its arguments, got %q", got)
	}
	if got := tr("Services"); got != "Serviços" {
		t.Fatalf("expected the Portuguese text, got %q", got)
	}
	SetLanguage("en")
	if got := tr("Services"); got != "Services" || tr("%d queued · %d already sent\n", 1, 2) != "1 queued · 2 already sent\n" {
		t.Fatalf("unexpected English text %q", got)
	}
	if tr("100% sure") != "100% sure" {
		t.Fatal("a message without arguments must not be treated as a format string")
	}
}

func TestThePanelAndTheTabsRenderInEnglishByDefault(t *testing.T) {
	withLanguage(t, "en")
	m, _ := panelModel(t)
	view := m.View()
	for _, want := range []string{"1 Panel", "4 History", "Services", "Meme pool", "queued", "already sent",
		"switched off", "(changed)", "paused", "space toggle", "quit"} {
		if !strings.Contains(view, want) {
			t.Errorf("English view is missing %q:\n%s", want, view)
		}
	}
}

// portugueseWords are common words that must never show up in the English interface. The
// data the fake orchestrator returns is English, so any hit is text that skipped tr().
var portugueseWords = strings.Fields(`nao não para uma serviço serviços fila enviados enviar canal canais
	ligado desligado pausado retoma roda rodar mover sair abas perfil perfis apagar apagado salvo nunca espaço aplica
	cancela continua todos fecha novo agora intervalo próxima próximas última rodou pulado erro ativo carregando
	atualizado atualizando falhou banco ambiente orquestrador filtro seguro prévia vídeo título detecções imagem
	mensagem texto histórico painel disparar sortear página alterado padrão ainda sem mas dos das está são`)

func portugueseIn(view string) []string {
	forbidden := map[string]bool{}
	for _, word := range portugueseWords {
		forbidden[word] = true
	}
	seen := map[string]bool{}
	words := strings.FieldsFunc(strings.ToLower(view), func(r rune) bool { return !unicode.IsLetter(r) })
	for _, word := range words {
		if forbidden[word] {
			seen[word] = true
		}
	}
	found := make([]string, 0, len(seen))
	for word := range seen {
		found = append(found, word)
	}
	sort.Strings(found)
	return found
}

func TestNoScreenOrOverlayLeaksPortugueseIntoTheEnglishInterface(t *testing.T) {
	withLanguage(t, "en")
	screens := map[string]func(t *testing.T) string{
		"panel": func(t *testing.T) string { m, _ := panelModel(t); return m.View() },
		"service toggle": func(t *testing.T) string {
			m, _ := panelModel(t)
			press(m, "down", "space")
			return m.View()
		},
		"run job":            func(t *testing.T) string { m, _ := panelModel(t); selectJob(m, 1); press(m, "x"); return m.View() },
		"interval":           func(t *testing.T) string { m, _ := panelModel(t); selectJob(m, 0); press(m, "i"); return m.View() },
		"profiles":           func(t *testing.T) string { m, _, _ := profileTUI(t); press(m, "p"); return m.View() },
		"new profile":        func(t *testing.T) string { m, _, _ := profileTUI(t); press(m, "p", "n"); return m.View() },
		"delete profile":     func(t *testing.T) string { m, _, _ := profileTUI(t); press(m, "p", "d"); return m.View() },
		"memes":              func(t *testing.T) string { m, _ := started(t); press(m, "2"); return m.View() },
		"meme send":          func(t *testing.T) string { m, _ := started(t); press(m, "2", "enter"); return m.View() },
		"meme dispatch":      func(t *testing.T) string { m, _ := started(t); press(m, "2", "d"); return m.View() },
		"discord":            func(t *testing.T) string { m, _ := started(t); press(m, "3"); return m.View() },
		"discord empty send": func(t *testing.T) string { m, _ := started(t); press(m, "3", "ctrl+s"); return m.View() },
		"history":            func(t *testing.T) string { m, _ := started(t); press(m, "4"); return m.View() },
	}
	names := make([]string, 0, len(screens))
	for name := range screens {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		view := screens[name](t)
		if strings.TrimSpace(view) == "" {
			t.Errorf("%s rendered nothing", name)
		}
		if leaked := portugueseIn(view); len(leaked) > 0 {
			t.Errorf("%s shows Portuguese words in English mode %v:\n%s", name, leaked, view)
		}
	}
}
