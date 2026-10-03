package digest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

type fakeService struct {
	reply    map[string]any
	err      error
	types    []string
	payloads []map[string]any
}

func (f *fakeService) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	f.types = append(f.types, eventType)
	f.payloads = append(f.payloads, payload)
	return f.reply, f.err
}

// entry is one item as the service sends it. hoursAgo below zero means no date.
func entry(id, source, title string, hoursAgo int, address string) map[string]any {
	published := ""
	if hoursAgo >= 0 {
		published = now.Add(-time.Duration(hoursAgo) * time.Hour).Format(time.RFC3339)
	}
	return map[string]any{
		"id": id, "plugin": "plugin-" + strings.ToLower(source), "title": title, "summary": "About " + title,
		"url": address, "published": published, "source": source,
	}
}

// itemsReply passes the entries through JSON, the way the orchestrator's client hands them over.
func itemsReply(t *testing.T, entries ...map[string]any) map[string]any {
	t.Helper()
	if entries == nil {
		entries = []map[string]any{}
	}
	body, err := json.Marshal(map[string]any{"items": entries, "total": len(entries), "updated": "2026-10-02T11:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	var reply map[string]any
	if err := json.Unmarshal(body, &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

func readDigest(t *testing.T, reply map[string]any, payload map[string]any) map[string]any {
	t.Helper()
	service := &fakeService{reply: reply}
	got, err := New(service).WithClock(func() time.Time { return now }).
		Handle(context.Background(), Request{Action: ActionReadDigest, Event: Event{Payload: payload}})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func titlesOf(reply map[string]any) []string {
	var titles []string
	for _, shown := range reply["items"].([]map[string]any) {
		titles = append(titles, shown["title"].(string))
	}
	return titles
}

func expectTitles(t *testing.T, reply map[string]any, want ...string) {
	t.Helper()
	if got := titlesOf(reply); !reflect.DeepEqual(got, want) {
		t.Fatalf("titles = %v, want %v", got, want)
	}
}

func TestEachActionAsksTheFeedsServiceForItsEvent(t *testing.T) {
	status := &fakeService{reply: map[string]any{"plugins": []any{}}}
	got, err := New(status).Handle(context.Background(), Request{Action: ActionInspectDigest})
	if err != nil || !reflect.DeepEqual(got, status.reply) {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if !reflect.DeepEqual(status.types, []string{"feeds.status"}) || status.payloads[0] != nil {
		t.Fatalf("status asked %v with %v", status.types, status.payloads)
	}

	items := &fakeService{reply: itemsReply(t)}
	if _, err := New(items).Handle(context.Background(), Request{Action: ActionReadDigest}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(items.types, []string{"feeds.items"}) || items.payloads[0] != nil {
		t.Fatalf("read asked %v with %v", items.types, items.payloads)
	}
}

func TestTheDomainRefusesWhatItCannotDo(t *testing.T) {
	if _, err := (*Domain)(nil).Handle(context.Background(), Request{Action: ActionReadDigest}); err == nil {
		t.Error("a nil domain answered")
	}
	if _, err := New(nil).Handle(context.Background(), Request{Action: ActionReadDigest}); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("a domain without a service: %v", err)
	}
	service := &fakeService{}
	_, err := New(service).Handle(context.Background(), Request{Action: "refresh-digest", Event: Event{Type: "digest.refresh"}})
	if err == nil || !strings.Contains(err.Error(), "unsupported digest action") || len(service.types) != 0 {
		t.Errorf("an unknown action: err=%v calls=%v", err, service.types)
	}
}

func TestAFailureOfTheServiceIsPassedOn(t *testing.T) {
	boom := errors.New("feeds service is down")
	_, err := New(&fakeService{err: boom}).Handle(context.Background(), Request{Action: ActionReadDigest})
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	for name, reply := range map[string]map[string]any{
		"no items key":     {"total": 0},
		"items not a list": {"items": "none"},
		"items an object":  {"items": map[string]any{}},
	} {
		_, err = New(&fakeService{reply: reply}).Handle(context.Background(), Request{Action: ActionReadDigest})
		if err == nil || !strings.Contains(err.Error(), "no item list") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestNoItemsIsAnEmptyDigest(t *testing.T) {
	for name, reply := range map[string]map[string]any{
		"an empty list": {"items": []any{}},
		"null":          {"items": nil},
	} {
		t.Run(name, func(t *testing.T) {
			got := readDigest(t, reply, nil)
			if len(titlesOf(got)) != 0 || got["total"] != 0 || got["more"] != 0 || got["sources"] != 0 {
				t.Fatalf("got = %v", got)
			}
		})
	}
}

func TestItemsComeNewestFirstAndUndatedOnesLast(t *testing.T) {
	reply := itemsReply(t,
		entry("a", "S1", "Five hours", 5, ""),
		entry("b", "S2", "One hour", 1, ""),
		entry("c", "S3", "No date first", -1, ""),
		entry("d", "S4", "Three hours", 3, ""),
		entry("e", "S5", "No date second", -1, ""),
	)
	expectTitles(t, readDigest(t, reply, nil), "One hour", "Three hours", "Five hours", "No date first", "No date second")
}

func TestAnItemOlderThanTheWindowIsDroppedButAnUndatedOneStays(t *testing.T) {
	reply := itemsReply(t,
		entry("a", "S1", "Fresh", 2, ""),
		entry("b", "S2", "Edge of the window", 167, ""),
		entry("c", "S3", "Too old", 200, ""),
		entry("d", "S4", "Undated", -1, ""),
	)
	got := readDigest(t, reply, nil)
	expectTitles(t, got, "Fresh", "Edge of the window", "Undated")
	if got["older"] != 1 || got["total"] != 3 {
		t.Fatalf("older=%v total=%v", got["older"], got["total"])
	}

	got = readDigest(t, reply, map[string]any{"max_age_hours": 24.0})
	expectTitles(t, got, "Fresh", "Undated")
	if got["older"] != 2 {
		t.Fatalf("older with a day window = %v", got["older"])
	}
}

func TestTheSameEntryIsShownOnceAndTheNewestCopyStays(t *testing.T) {
	title := "The harbour bridge reopens after repairs"
	reply := itemsReply(t,
		entry("1", "S1", title, 6, "http://www.Example.com/entries/bridge/?utm_source=a&utm_medium=b#top"),
		entry("2", "S2", "Something else entirely different", 5, "https://example.com/other"),
		entry("3", "S3", "THE HARBOUR BRIDGE, reopens after repairs!", 2, "https://example.org/another-address"),
		entry("4", "S4", "An entry seen at a second address", 4, "https://example.com/entries/bridge"),
	)
	got := readDigest(t, reply, nil)
	// 3 is the newest copy of the bridge entry (the same title as 1); 4 is the same page as 1 (the
	// same address). Item 1 is older than both, so it is the one dropped, and it counts once.
	expectTitles(t, got, "THE HARBOUR BRIDGE, reopens after repairs!", "An entry seen at a second address", "Something else entirely different")
	if got["duplicates"] != 1 {
		t.Fatalf("duplicates = %v", got["duplicates"])
	}
}

func TestAddressesThatDifferOnlyInNoiseAreTheSamePage(t *testing.T) {
	reply := itemsReply(t,
		entry("1", "S1", "First title is long enough", 1, "https://www.example.com/a/b/?utm_campaign=x&fbclid=y"),
		entry("2", "S2", "Second title is long enough", 2, "http://EXAMPLE.com/a/b#comments"),
		entry("3", "S3", "Third title is long enough", 3, "https://example.com/a/b?gclid=1"),
		entry("4", "S4", "Fourth title is long enough", 4, "https://example.com/a/b?id=7"),
		entry("5", "S5", "Fifth title is long enough", 5, "https://example.com/a/b?id=8"),
	)
	got := readDigest(t, reply, nil)
	expectTitles(t, got, "First title is long enough", "Fourth title is long enough", "Fifth title is long enough")
	if got["duplicates"] != 2 {
		t.Fatalf("duplicates = %v", got["duplicates"])
	}
}

func TestShortTitlesAreOnlyMatchedByAddress(t *testing.T) {
	reply := itemsReply(t,
		entry("1", "S1", "Update", 1, "https://example.com/one"),
		entry("2", "S2", "Update", 2, "https://example.com/two"),
		entry("3", "S3", "Update", 3, ""),
	)
	expectTitles(t, readDigest(t, reply, nil), "Update", "Update", "Update")
}

func TestNoSourceFloodsTheFirstPartOfTheDigest(t *testing.T) {
	var entries []map[string]any
	for hour := 1; hour <= 5; hour++ {
		entries = append(entries, entry(fmt.Sprint("a", hour), "Source A", fmt.Sprintf("A%d", hour), hour, ""))
	}
	entries = append(entries,
		entry("b1", "Source B", "B1", 6, ""),
		entry("b2", "source b", "B2", 7, ""),
	)
	reply := itemsReply(t, entries...)

	expectTitles(t, readDigest(t, reply, nil), "A1", "A2", "A3", "B1", "B2", "A4", "A5")
	expectTitles(t, readDigest(t, reply, map[string]any{"per_source": 1.0}), "A1", "B1", "A2", "A3", "A4", "A5", "B2")
	expectTitles(t, readDigest(t, reply, map[string]any{"per_source": 10.0}), "A1", "A2", "A3", "A4", "A5", "B1", "B2")
}

func TestASourceWithoutANameIsGroupedByItsPlugin(t *testing.T) {
	var entries []map[string]any
	for hour := 1; hour <= 4; hour++ {
		item := entry(fmt.Sprint(hour), "", fmt.Sprintf("N%d", hour), hour, "")
		item["plugin"] = "same"
		entries = append(entries, item)
	}
	entries = append(entries, entry("x", "Other", "O1", 5, ""))
	expectTitles(t, readDigest(t, itemsReply(t, entries...), nil), "N1", "N2", "N3", "O1", "N4")
}

func TestPagingNeverRepeatsOrSkipsAnItem(t *testing.T) {
	var entries []map[string]any
	for i := 0; i < 30; i++ {
		entries = append(entries, entry(fmt.Sprint(i), fmt.Sprint("S", i%4), fmt.Sprintf("Title %02d", i), i+1, ""))
	}
	reply := itemsReply(t, entries...)

	var seen []string
	for offset := 0; ; offset += 7 {
		page := readDigest(t, reply, map[string]any{"limit": 7.0, "offset": float64(offset)})
		seen = append(seen, titlesOf(page)...)
		if page["total"] != 30 || page["offset"] != min(offset, 30) {
			t.Fatalf("total=%v offset=%v", page["total"], page["offset"])
		}
		if page["more"] != max(0, 30-offset-7) {
			t.Fatalf("offset %d: more = %v", offset, page["more"])
		}
		if page["more"] == 0 {
			break
		}
	}
	whole := titlesOf(readDigest(t, reply, map[string]any{"limit": 50.0}))
	if !reflect.DeepEqual(seen, whole) || len(whole) != 30 {
		t.Fatalf("pages gave %d items, the whole digest %d", len(seen), len(whole))
	}
}

func TestAnOffsetPastTheEndGivesAnEmptyPage(t *testing.T) {
	reply := itemsReply(t, entry("1", "S1", "Only one", 1, ""))
	got := readDigest(t, reply, map[string]any{"offset": 500.0})
	if len(titlesOf(got)) != 0 || got["offset"] != 1 || got["more"] != 0 || got["total"] != 1 {
		t.Fatalf("got = %v", got)
	}
}

func TestWhatACallerAsksForIsClampedOrRefused(t *testing.T) {
	var entries []map[string]any
	for i := 0; i < 60; i++ {
		entries = append(entries, entry(fmt.Sprint(i), "S", fmt.Sprintf("Distinct title number %02d here", i), i+1, ""))
	}
	reply := itemsReply(t, entries...)

	for name, c := range map[string]struct {
		payload map[string]any
		count   int
		offset  int
	}{
		"no payload":         {nil, 12, 0},
		"explicit nulls":     {map[string]any{"limit": nil, "offset": nil}, 12, 0},
		"a limit of zero":    {map[string]any{"limit": 0.0}, 1, 0},
		"a huge limit":       {map[string]any{"limit": 1e9}, 50, 0},
		"a negative offset":  {map[string]any{"offset": -5.0}, 12, 0},
		"a json number":      {map[string]any{"limit": json.Number("5")}, 5, 0},
		"an int":             {map[string]any{"limit": 3, "offset": int64(2)}, 3, 2},
		"an offset in range": {map[string]any{"offset": 55.0}, 5, 55},
		"a huge offset":      {map[string]any{"offset": 1e9}, 0, 60},
		"unknown field":      {map[string]any{"colour": "blue"}, 12, 0},
	} {
		t.Run(name, func(t *testing.T) {
			got := readDigest(t, reply, c.payload)
			if len(titlesOf(got)) != c.count || got["offset"] != c.offset {
				t.Fatalf("count=%d offset=%v, want %d and %d", len(titlesOf(got)), got["offset"], c.count, c.offset)
			}
		})
	}

	for name, payload := range map[string]map[string]any{
		"a text limit":       {"limit": "ten"},
		"a fractional limit": {"limit": 1.5},
		"a boolean offset":   {"offset": true},
		"a list":             {"per_source": []any{1}},
		"a bad json number":  {"max_age_hours": json.Number("x")},
	} {
		t.Run(name, func(t *testing.T) {
			service := &fakeService{reply: reply}
			_, err := New(service).Handle(context.Background(), Request{Action: ActionReadDigest, Event: Event{Payload: payload}})
			if err == nil || !strings.Contains(err.Error(), "whole number") {
				t.Fatalf("err=%v", err)
			}
			if len(service.types) != 0 {
				t.Fatal("the service was asked for a request that is refused")
			}
		})
	}
}

func TestADateWrittenWithAnOffsetIsShownInUTC(t *testing.T) {
	reply := map[string]any{"items": []any{
		map[string]any{"title": "Dated in another zone", "published": "2026-10-02T09:30:00+03:00"},
	}}
	got := readDigest(t, reply, nil)
	if shown := got["items"].([]map[string]any)[0]["published"]; shown != "2026-10-02T06:30:00Z" {
		t.Fatalf("published = %v", shown)
	}
}

func TestAnEntryThatIsNotAnItemIsSkipped(t *testing.T) {
	reply := map[string]any{"items": []any{
		"text", nil, 5.0, map[string]any{"summary": "no title"}, map[string]any{"title": "  "},
		map[string]any{"title": 7.0}, map[string]any{"title": "Good", "published": 12.0, "url": []any{}},
	}}
	got := readDigest(t, reply, nil)
	expectTitles(t, got, "Good")
	if got["total"] != 1 {
		t.Fatalf("total = %v", got["total"])
	}
}

func TestTheReplyCarriesWhatTheScreenNeeds(t *testing.T) {
	reply := itemsReply(t,
		entry("1", "Alpha", "A title that is long enough", 2, "https://example.com/1"),
		entry("2", "Beta", "A title that is long enough!", 3, ""),
		entry("3", "Beta", "Old one", 300, ""),
		entry("4", "Gamma", "Undated", -1, ""),
	)
	got := readDigest(t, reply, nil)
	if got["total"] != 2 || got["more"] != 0 || got["sources"] != 2 || got["duplicates"] != 1 || got["older"] != 1 ||
		got["offset"] != 0 || got["updated"] != "2026-10-02T11:00:00Z" {
		// 1 and 4 remain: 2 duplicates 1 by title, 3 is old
		t.Fatalf("reply = %v", got)
	}
	first := got["items"].([]map[string]any)[0]
	want := map[string]any{
		"id": "1", "plugin": "plugin-alpha", "title": "A title that is long enough", "summary": "About A title that is long enough",
		"url": "https://example.com/1", "published": "2026-10-02T10:00:00Z", "source": "Alpha",
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("first = %v", first)
	}
	if last := got["items"].([]map[string]any)[1]; last["published"] != "" {
		t.Fatalf("an undated item has published %q", last["published"])
	}
}
