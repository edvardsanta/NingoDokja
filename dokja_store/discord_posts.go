package store

import (
	"database/sql"
	"fmt"
)

// DiscordPost identifies one Discord message Ningo posted. Recorded per attached
// media URL so a later citation can link the message itself, not just the source
// page the media came from.
type DiscordPost struct {
	GuildID   string
	ChannelID string
	MessageID string
}

// Permalink is the Discord client link to the message. It only opens for people
// who can already see the channel, so it reveals nothing to outsiders.
func (p DiscordPost) Permalink() string {
	return fmt.Sprintf("https://discord.com/channels/%s/%s/%s", p.GuildID, p.ChannelID, p.MessageID)
}

// RecordDiscordPost remembers that mediaURL was posted as this Discord message.
// Reposts keep their own row; the latest one wins a lookup.
func (d *DB) RecordDiscordPost(mediaURL, guildID, channelID, messageID string) error {
	if mediaURL == "" || channelID == "" || messageID == "" {
		return fmt.Errorf("media url, channel and message are required")
	}
	_, err := d.db.Exec(
		`INSERT INTO discord_posts(media_url, guild_id, channel_id, message_id, posted_at)
		 VALUES(?, ?, ?, ?, ?)
		 ON CONFLICT(media_url, channel_id, message_id) DO UPDATE SET posted_at = excluded.posted_at`,
		mediaURL, guildID, channelID, messageID, now())
	return err
}

// LatestDiscordPost returns the most recent recorded post of mediaURL.
func (d *DB) LatestDiscordPost(mediaURL string) (DiscordPost, bool, error) {
	var p DiscordPost
	err := d.db.QueryRow(
		`SELECT guild_id, channel_id, message_id FROM discord_posts
		 WHERE media_url = ? ORDER BY posted_at DESC, rowid DESC LIMIT 1`,
		mediaURL).Scan(&p.GuildID, &p.ChannelID, &p.MessageID)
	if err == sql.ErrNoRows {
		return DiscordPost{}, false, nil
	}
	if err != nil {
		return DiscordPost{}, false, err
	}
	return p, true, nil
}
