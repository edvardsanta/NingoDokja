package handlers

import "testing"

func TestStripReplyPreamble(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"leaked header", "Ningo's reply to draco's bare \"jo\" (no tools used):\n\njo? Isso é uma sílaba.", "jo? Isso é uma sílaba."},
		{"curly apostrophe and bold", "**Ningo’s reply to Pedro (no tools used):**\nOk.", "Ok."},
		{"ordinary reply is untouched", "jo? Isso é uma sílaba.", "jo? Isso é uma sílaba."},
		{"mid-reply mention is untouched", "Antes.\nNingo's reply to draco (no tools used):\nDepois.", "Antes.\nNingo's reply to draco (no tools used):\nDepois."},
		{"a reply that talks about replies", "Reply to the board is due Friday.", "Reply to the board is due Friday."},
		{"only the header", "Ningo's reply to draco (no tools used):", "Ningo's reply to draco (no tools used):"},
	}
	for _, c := range cases {
		if got := stripReplyPreamble(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
