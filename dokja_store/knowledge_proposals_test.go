package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func proposalRow(id, by string) KnowledgeProposal {
	return KnowledgeProposal{
		ID: id, Kind: "web_article", Payload: `{"url":"https://example.com/a"}`,
		ProposedBy: by, SentBy: by, SourceChannel: "memes", SourceMessage: "m1",
		SourcePermalink: "https://example.com/ch/1/m1", ProposedAt: now(),
	}
}

func TestProposalsSurviveAMigrationFromAnOlderDatabase(t *testing.T) {
	path := filepathFor(t)
	// Build a real version-2 database: the first two migrations, no proposals table.
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := raw.Exec(migrations[i]); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
	}
	if _, err := raw.Exec(`PRAGMA user_version = 2`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	migrated, err := Open(path)
	if err != nil {
		t.Fatalf("an older database must open with the proposals table added: %v", err)
	}
	defer migrated.Close()
	if err := migrated.AddProposal(proposalRow("proposal:web_article:one", "sam")); err != nil {
		t.Fatalf("the migrated database keeps no proposals but accepts them: %v", err)
	}
}

func TestProposalsAddListGetDeleteAndConflict(t *testing.T) {
	db := open(t)
	row := proposalRow("proposal:web_article:one", "sam")
	if err := db.AddProposal(row); err != nil {
		t.Fatal(err)
	}
	if err := db.AddProposal(row); err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("a repeated proposal id must conflict, got %v", err)
	}
	if err := db.AddProposal(KnowledgeProposal{ID: "x"}); err == nil {
		t.Fatal("an incomplete proposal was accepted")
	}

	pending, err := db.ListProposals()
	if err != nil || len(pending) != 1 || pending[0].ID != row.ID {
		t.Fatalf("pending proposals lost: %v %v", pending, err)
	}
	got, ok, err := db.GetProposal(row.ID)
	if err != nil || !ok || got.Payload != row.Payload || got.SourceChannel != "memes" {
		t.Fatalf("a proposal did not round trip: %+v %v", got, err)
	}
	if _, ok, err := db.GetProposal("proposal:none"); err != nil || ok {
		t.Fatalf("a missing proposal must be absent, not an error: %v", err)
	}

	if count, err := db.CountProposalsBy("sam"); err != nil || count != 1 {
		t.Fatalf("per-proposer count wrong: %d %v", count, err)
	}
	if count, err := db.CountProposalsBy("nobody"); err != nil || count != 0 {
		t.Fatalf("per-proposer count wrong: %d %v", count, err)
	}

	if err := db.DeleteProposal(row.ID); err != nil {
		t.Fatal(err)
	}
	if pending, err := db.ListProposals(); err != nil || len(pending) != 0 {
		t.Fatalf("a decision must end the proposal: %v %v", pending, err)
	}
}

func TestProposalsListOldestFirst(t *testing.T) {
	db := open(t)
	first := proposalRow("proposal:web_article:first", "sam")
	second := proposalRow("proposal:web_article:second", "sam")
	second.ProposedAt = first.ProposedAt + "1"
	if err := db.AddProposal(second); err != nil {
		t.Fatal(err)
	}
	if err := db.AddProposal(first); err != nil {
		t.Fatal(err)
	}
	pending, err := db.ListProposals()
	if err != nil || len(pending) != 2 || pending[0].ID != first.ID {
		t.Fatalf("pending proposals are not oldest first: %v %v", pending, err)
	}
}

func filepathFor(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "dokja.db")
}
