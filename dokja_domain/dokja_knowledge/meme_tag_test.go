package knowledge

import "testing"

func TestParseMemeTagPost(t *testing.T) {
	cases := []struct {
		name           string
		content        string
		media, isReply bool
		wantTag        string
		wantForm       MemeTagForm
		wantLink       string
		wantProposed   bool
	}{
		{"a link and a tag", "https://example.com/clip\n\n#SomeGames", false, false, "#SomeGames", MemeTagDirect, "https://example.com/clip", true},
		{"an attachment and a tag", "#SomeGames", true, false, "#SomeGames", MemeTagDirect, "", true},
		{"a reply naming the tag", "#SomeGames", false, true, "#SomeGames", MemeTagReply, "", true},
		{"a reply with a word around it", "isso é #SomeGames", false, true, "#SomeGames", MemeTagReply, "", true},
		{"a tag alone is not a proposal", "#SomeGames", false, false, "", "", "", false},
		{"no tag", "https://example.com/clip", true, false, "", "", "", false},
		{"a link to a chat message is not the meme", "#SomeGames https://discord.com/channels/guild-1/chan-1/msg-1", false, false, "", "", "", false},
		{"a page anchor is not a tag", "https://example.com/doc#section", false, false, "", "", "", false},
		{"a tag inside a word is not a tag", "a#b", true, false, "", "", "", false},
		{"the first tag wins", "#First #Second", true, false, "#First", MemeTagDirect, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			post, ok := ParseMemeTagPost(c.content, c.media, c.isReply)
			if ok != c.wantProposed || post.Hashtag != c.wantTag || post.Form != c.wantForm || post.Link != c.wantLink {
				t.Fatalf("got %#v %v", post, ok)
			}
		})
	}
}

func TestAMemeAddressDoesNotKeepWhatMakesALinkExpire(t *testing.T) {
	signed := "https://cdn.example/attachments/chan-1/msg-1/clip.mp4?ex=abc&is=def&hm=ghi"
	if got := StableMediaRef(signed); got != "https://cdn.example/attachments/chan-1/msg-1/clip.mp4" {
		t.Fatal(got)
	}
	if got := StableMediaRef("https://example.com/watch?v=abc"); got != "https://example.com/watch?v=abc" {
		t.Fatal("a page link keeps its query, it names the page:", got)
	}
	if !LinkIsMediaFile("https://example.com/a/b.JPG") || LinkIsMediaFile("https://example.com/watch?v=abc") {
		t.Fatal("only a link to a file is a media file")
	}
}
