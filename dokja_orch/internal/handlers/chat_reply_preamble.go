package handlers

import "regexp"

// replyPreamble matches a line the model sometimes writes before its reply, narrating the reply
// instead of giving it: "Ningo's reply to draco's bare "jo" (no tools used):". It is bookkeeping
// from the model, never something a person in the channel should read.
var replyPreamble = regexp.MustCompile(`(?i)\A\s*(?:\*\*)?(?:[\p{L}\p{N}_ ]{1,40}['’]s\s+)?reply\s+to\b[^\n]{0,200}?\((?:no|without)\s+tools?(?:\s+used)?\)\s*:?(?:\*\*)?[ \t]*(?:\r?\n)+`)

// stripReplyPreamble drops that narrating line from the start of a reply. A reply that is only
// the preamble is returned unchanged so an empty reply is still reported as one by the caller.
func stripReplyPreamble(reply string) string {
	stripped := replyPreamble.ReplaceAllString(reply, "")
	if stripped == "" {
		return reply
	}
	return stripped
}
