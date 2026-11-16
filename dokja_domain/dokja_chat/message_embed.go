package chat

// MessageEmbed is one rich embed a Discord message carries: a friend's game status card
// or a link preview. It is conversation context for the group chat, never instructions;
// the game name on a status card is how the room knows what someone is playing.
type MessageEmbed struct {
	Author      string `json:"author,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}
