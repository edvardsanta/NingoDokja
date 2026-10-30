package memory

import "testing"

func TestAListenIsPerPersonAndSettledWithASharedOutcome(t *testing.T) {
	endings := map[string]string{ListenFinished: OutcomeAccepted, ListenSkipped: OutcomeReplaced, ListenStopped: OutcomeIgnored}
	for ended, outcome := range endings {
		listen, ok := PrepareMusicListen("epoch/guild/1", "42", " Monalisa ", " Jorge ", ended)
		if !ok {
			t.Fatal(ended)
		}
		record := listen.RecordRequest().Event.Payload
		if record["action"] != "music.listen:42" || record["context"] != "Monalisa – Jorge" || record["detail"] != ended {
			t.Fatalf("%#v", record)
		}
		if _, err := buildRecord(record); err != nil {
			t.Fatal("Memory accepts the record", err)
		}
		resolve := listen.ResolveRequest().Event.Payload
		if resolve["outcome"] != outcome || resolve["ref"] != record["ref"] || !validOutcome(outcome) {
			t.Fatalf("%#v", resolve)
		}
	}
}

func TestTheSamePlayingKeepsItsRefAndAnotherGetsItsOwn(t *testing.T) {
	first, _ := PrepareMusicListen("epoch/guild/1", "42", "One", "", ListenFinished)
	again, _ := PrepareMusicListen("epoch/guild/1", "42", "One", "", ListenFinished)
	other, _ := PrepareMusicListen("epoch/guild/2", "42", "One", "", ListenFinished)
	if first.ref != again.ref || first.ref == other.ref {
		t.Fatal(first.ref, again.ref, other.ref)
	}
}

func TestNoListenWithoutAPersonASongOrAKnownEnding(t *testing.T) {
	for _, args := range [][5]string{
		{"play", "", "One", "", ListenFinished},
		{"play", "music.listen:42", "One", "", ListenFinished},
		{"play", "42", " ", "", ListenFinished},
		{"play", "42", "One", "", "paused"},
		{" ", "42", "One", "", ListenFinished},
	} {
		if _, ok := PrepareMusicListen(args[0], args[1], args[2], args[3], args[4]); ok {
			t.Fatal(args)
		}
	}
}

func TestAHistoryIsAskedOnlyForOneKnownPerson(t *testing.T) {
	if _, ok := MusicHistoryRequest("42 OR 43", 10); ok {
		t.Fatal("a person is an id")
	}
	request, ok := MusicHistoryRequest("42", 10)
	if !ok || request.Action != ActionListExperience {
		t.Fatal(request)
	}
	clean, err := buildList(request.Event.Payload)
	if err != nil || clean["action"] != "music.listen:42" || clean["state"] != "resolved" || clean["limit"] != 10 {
		t.Fatal(clean, err)
	}
	plays := MusicHistory(map[string]any{"experiences": []any{
		map[string]any{"detail": "skipped", "context_snippet": "One – Band"},
		map[string]any{"detail": "finished"},
	}})
	if len(plays) != 1 || plays[0] != (MusicPlay{Song: "One – Band", Ended: "skipped"}) {
		t.Fatal(plays)
	}
}
