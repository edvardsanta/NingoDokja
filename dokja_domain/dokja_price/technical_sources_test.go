package price

import (
	"strings"
	"testing"
)

func TestCollectedTechnicalSourcesKeepAttributionAndRejectMalformedEvidence(t *testing.T) {
	source := map[string]any{"title": "Example test lab", "url": "https://bench.example/review", "observed_at": "2026-11-11T10:00:00Z", "workload": "1080p gaming", "excerpt": "The tested cards reached similar results in this workload."}
	specs, err := cleanSpecifications(map[string]any{"technical_sources": []any{source}, "cores": float64(8)})
	if err != nil {
		t.Fatal(err)
	}
	product := map[string]any{"product": "reference", "specifications": specs}
	rows := ComparisonSources(map[string]any{"product": product, "alternatives": []any{}})
	if len(rows) != 1 || rows[0].(map[string]any)["product"] != "reference" || rows[0].(map[string]any)["excerpt"] != source["excerpt"] {
		t.Fatal(rows)
	}
	for _, bad := range []map[string]any{
		{"title": "Missing date", "url": "https://bench.example/review"},
		{"title": "Invalid protocol", "url": "javascript:alert(1)", "observed_at": "2026-11-11T10:00:00Z"},
		{"title": "Credentials", "url": "https://private:secret@bench.example/review", "observed_at": "2026-11-11T10:00:00Z"},
		{"title": "Oversized", "url": "https://bench.example/review", "observed_at": "2026-11-11T10:00:00Z", "excerpt": strings.Repeat("x", 1201)},
	} {
		if _, err := cleanSpecifications(map[string]any{"technical_sources": []any{bad}}); err == nil {
			t.Fatal("invalid source accepted", bad)
		}
	}
}
func TestComparisonContextIsOptionalAndBounded(t *testing.T) {
	old, err := compareFromPayload(map[string]any{"query": "example"})
	if err != nil || old.IntendedUse != "" {
		t.Fatal(old, err)
	}
	c, err := compareFromPayload(map[string]any{"query": "example", "intended_use": "1080p gaming", "current_equipment": "CPU Example / 500W"})
	if err != nil || c.validate() != nil {
		t.Fatal(c, err)
	}
	result := Comparison{Found: true, IntendedUse: c.IntendedUse, CurrentEquipment: c.CurrentEquipment}.Map()
	if result["comparison_context"].(map[string]any)["current_equipment"] != c.CurrentEquipment {
		t.Fatal(result)
	}
	c.IntendedUse = strings.Repeat("a", 501)
	if c.validate() == nil {
		t.Fatal("unbounded context")
	}
	if _, err := compareFromPayload(map[string]any{"query": "example", "current_equipment": true}); err == nil {
		t.Fatal("nontext context accepted")
	}
}
