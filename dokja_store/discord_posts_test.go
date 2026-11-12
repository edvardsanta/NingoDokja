package store

import "testing"

func TestDiscordPostsRecordAndResolveTheLatestPermalink(t *testing.T) {
	db := open(t)

	if _, ok, err := db.LatestDiscordPost("https://memes.example/a.mp4"); err != nil || ok {
		t.Fatalf("an unposted media has no permalink: %v %t", err, ok)
	}

	if err := db.RecordDiscordPost("https://memes.example/a.mp4", "guild-1", "chan-1", "msg-1"); err != nil {
		t.Fatal(err)
	}
	post, ok, err := db.LatestDiscordPost("https://memes.example/a.mp4")
	if err != nil || !ok {
		t.Fatalf("expected the recorded post: %v %t", err, ok)
	}
	if got, want := post.Permalink(), "https://discord.com/channels/guild-1/chan-1/msg-1"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// A repost supersedes the earlier message.
	if err := db.RecordDiscordPost("https://memes.example/a.mp4", "guild-1", "chan-2", "msg-2"); err != nil {
		t.Fatal(err)
	}
	post, ok, err = db.LatestDiscordPost("https://memes.example/a.mp4")
	if err != nil || !ok || post.MessageID != "msg-2" {
		t.Fatalf("expected the latest post, got %+v %t %v", post, ok, err)
	}

	// Recording the same delivery twice is idempotent.
	if err := db.RecordDiscordPost("https://memes.example/a.mp4", "guild-1", "chan-2", "msg-2"); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordDiscordPost("", "guild-1", "chan-1", "msg-1"); err == nil {
		t.Fatal("empty media url must be refused")
	}
}
