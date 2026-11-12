package price

import (
	"math"
	"strings"
	"testing"
	"time"
)

func benchmarkSource(score float64, snapshot string) map[string]any {
	return map[string]any{"title": "Example lab", "url": "https://bench.example/report", "observed_at": time.Now().UTC().Format(time.RFC3339), "published_at": "2026-01-01", "updated_at": "2026-02-01T00:00:00Z", "excerpt": "GPU row and measured result", "test_conditions": "Example CPU; driver 1; native 1080p Ultra", "benchmarks": []any{map[string]any{"test": "Raster / 1080p Ultra", "unit": "fps", "value": score, "snapshot": snapshot, "sample": "Example GPU", "higher_is_better": true}}}
}
func benchmarkComparison() map[string]any {
	return map[string]any{"product": map[string]any{"product": "ref"}, "alternatives": []any{map[string]any{"product": "other"}}}
}
func TestBenchmarkEvidenceIsValidatedAndComparedOnlyWithinOneSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(map[string]any)
		compare bool
	}{
		{"same test", func(s map[string]any) {}, true},
		{"different snapshot", func(s map[string]any) {
			s["benchmarks"].([]any)[0].(map[string]any)["snapshot"] = strings.Repeat("b", 64)
		}, false},
		{"different conditions", func(s map[string]any) { s["test_conditions"] = "Another driver or resolution" }, false},
		{"different suite", func(s map[string]any) {
			s["benchmarks"].([]any)[0].(map[string]any)["test"] = "Ray tracing / 1080p Ultra"
		}, false},
		{"different source", func(s map[string]any) { s["url"] = "https://other.example/report" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := benchmarkSource(100, strings.Repeat("a", 64))
			b := benchmarkSource(110, strings.Repeat("a", 64))
			tc.change(b)
			result := benchmarkComparison()
			rows, err := ApplyTechnicalEvidence(result, []any{map[string]any{"product_key": "ref", "technical_sources": []any{a}, "opinion": "do not cache"}, map[string]any{"product_key": "other", "technical_sources": []any{b}}})
			if err != nil || rows[0]["opinion"] != nil {
				t.Fatal(rows, err)
			}
			AttachBenchmarkComparisons(result)
			candidate := result["alternatives"].([]any)[0].(map[string]any)
			comparisons, _ := candidate["benchmark_comparisons"].([]any)
			if (len(comparisons) > 0) != tc.compare {
				t.Fatal(comparisons)
			}
			if tc.compare && comparisons[0].(map[string]any)["difference_basis_points"] != int64(1000) {
				t.Fatal(comparisons)
			}
			if candidate["classification"] != nil {
				t.Fatal("one benchmark became universal equivalence")
			}
		})
	}
}
func TestBenchmarkEvidenceRejectsInvalidValuesAndIsAtomic(t *testing.T) {
	for _, value := range []any{math.NaN(), math.Inf(1), -1.0, 0.0, "100"} {
		source := benchmarkSource(100, strings.Repeat("a", 64))
		source["benchmarks"].([]any)[0].(map[string]any)["value"] = value
		if _, err := cleanTechnicalSources([]any{source}); err == nil {
			t.Fatal("invalid value accepted", value)
		}
	}
	result := benchmarkComparison()
	source := benchmarkSource(100, strings.Repeat("a", 64))
	_, err := ApplyTechnicalEvidence(result, []any{map[string]any{"product_key": "ref", "technical_sources": []any{source}}, map[string]any{"product_key": "unknown", "technical_sources": []any{source}}})
	if err == nil || result["product"].(map[string]any)["specifications"] != nil {
		t.Fatal("partially applied invalid evidence")
	}
}
func TestCollectedFactsExpireWithoutUsingPublicationAsCollectionTime(t *testing.T) {
	now := time.Now()
	source := benchmarkSource(100, strings.Repeat("a", 64))
	rows := []map[string]any{{"product_key": "ref", "technical_sources": []any{source}}}
	if !TechnicalEvidenceFresh(rows, now) {
		t.Fatal("fresh facts rejected")
	}
	source["observed_at"] = now.Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
	if TechnicalEvidenceFresh(rows, now) {
		t.Fatal("stale facts used")
	}
	source["observed_at"] = now.Add(time.Hour).UTC().Format(time.RFC3339)
	if TechnicalEvidenceFresh(rows, now) {
		t.Fatal("future collection time accepted")
	}
}

func TestBenchmarkSampleCannotStandInForDifferentModel(t *testing.T) {
	for _, titles := range [][2]string{{"RX 7600", "RX 7600 XT"}, {"Ryzen 7 9700X", "Ryzen 7 9700X3D"}, {"Core i5-14600K", "Core i5-14600KF"}, {"RTX 4060 Ti 8GB", "RTX 4060 Ti 16GB"}} {
		result := benchmarkComparison()
		result["product"].(map[string]any)["title"] = titles[0]
		source := benchmarkSource(100, strings.Repeat("a", 64))
		source["benchmarks"].([]any)[0].(map[string]any)["sample"] = titles[1]
		if _, err := ApplyTechnicalEvidence(result, []any{map[string]any{"product_key": "ref", "technical_sources": []any{source}}}); err == nil {
			t.Fatal("different sample accepted", titles)
		}
	}
}
