package ownerconfig

// DefaultWarmPath is where the operator describes how Ningo talks with the people marked "warm" in
// the owner configuration. It stays out of Git: it is about real people close to the operator.
const DefaultWarmPath = ".dokja/warm.md"

// LoadWarm reads that description as free text. An absent file leaves those people with Ningo's
// usual voice. Error messages deliberately omit the file's content.
func LoadWarm(path string) (string, error) {
	if path == "" {
		path = DefaultWarmPath
	}
	return loadPrivateText(path, "warm")
}
