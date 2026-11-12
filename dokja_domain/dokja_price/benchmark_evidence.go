package price

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

var benchmarkMemory = regexp.MustCompile(`(?i)\b(\d+)\s*gb\b`)

var benchmarkCPUModel = regexp.MustCompile(`(?i)\b(ryzen\s*[3579]\s+\d{4,5}(?:x3d|x|g|f)?|core\s+ultra\s+[3579]\s+\d{3}[a-z]{0,2}|core\s+i[3579][ -]?\d{4,5}[a-z]{0,2}|arc\s+[ab]\d{3})\b`)

func benchmarkModel(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	if model := gpuModel(title); model != "" {
		return model
	}
	return alnum(benchmarkCPUModel.FindString(title))
}

var benchmarkSnapshot = regexp.MustCompile(`^[a-f0-9]{64}$`)

func cleanBenchmarks(raw any) ([]any, error) {
	rows, ok := raw.([]any)
	if !ok || len(rows) > 8 {
		return nil, fmt.Errorf("invalid benchmark list")
	}
	clean := []any{}
	seen := map[string]bool{}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid benchmark")
		}
		test, _ := row["test"].(string)
		unit, _ := row["unit"].(string)
		sample, _ := row["sample"].(string)
		snapshot, _ := row["snapshot"].(string)
		value, ok := benchmarkValue(row["value"])
		higher, directionOK := row["higher_is_better"].(bool)
		if !ok || !directionOK || !benchmarkSnapshot.MatchString(snapshot) || strings.TrimSpace(test) == "" || len([]rune(test)) > 160 || strings.TrimSpace(sample) == "" || len([]rune(sample)) > 120 {
			return nil, fmt.Errorf("invalid benchmark measurement")
		}
		switch unit {
		case "fps", "score", "percent_index", "seconds", "ms":
		default:
			return nil, fmt.Errorf("unsupported benchmark unit")
		}
		key := test + "/" + snapshot
		if seen[key] {
			return nil, fmt.Errorf("duplicate benchmark")
		}
		seen[key] = true
		clean = append(clean, map[string]any{"test": test, "unit": unit, "sample": sample, "snapshot": snapshot, "value": value, "higher_is_better": higher})
	}
	return clean, nil
}
func benchmarkValue(raw any) (float64, bool) {
	value, ok := raw.(float64)
	if n, yes := raw.(json.Number); yes {
		var err error
		value, err = n.Float64()
		ok = err == nil
	}
	return value, ok && !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0 && value <= 1e9
}

// ApplyTechnicalEvidence accepts only facts about products already in this comparison.
// Extra fields (including model opinions) never enter the collected-fact cache.
func ApplyTechnicalEvidence(result map[string]any, raw any) ([]map[string]any, error) {
	products := map[string]map[string]any{}
	if p, ok := result["product"].(map[string]any); ok {
		key, _ := p["product"].(string)
		products[key] = p
	}
	alternatives, _ := result["alternatives"].([]any)
	for _, raw := range alternatives {
		if p, ok := raw.(map[string]any); ok {
			key, _ := p["product"].(string)
			products[key] = p
		}
	}
	rows, ok := raw.([]any)
	if !ok || len(rows) > 13 {
		return nil, fmt.Errorf("invalid technical evidence")
	}
	validated := []map[string]any{}
	seen := map[string]bool{}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid technical evidence product")
		}
		key, _ := row["product_key"].(string)
		if products[key] == nil || seen[key] {
			return nil, fmt.Errorf("unknown or duplicate evidence product")
		}
		seen[key] = true
		sources, err := cleanTechnicalSources(row["technical_sources"])
		if err != nil {
			return nil, err
		}
		title, _ := products[key]["title"].(string)
		expected := benchmarkModel(title)
		for _, raw := range sources {
			source := raw.(map[string]any)
			metrics, _ := source["benchmarks"].([]any)
			for _, raw := range metrics {
				metric := raw.(map[string]any)
				sample, _ := metric["sample"].(string)
				expectedMemory := benchmarkMemory.FindStringSubmatch(title)
				sampleMemory := benchmarkMemory.FindStringSubmatch(sample)
				if len(expectedMemory) > 0 && len(sampleMemory) > 0 && expectedMemory[1] != sampleMemory[1] {
					return nil, fmt.Errorf("benchmark memory does not match product")
				}
				if expected != "" && benchmarkModel(sample) != expected {
					return nil, fmt.Errorf("benchmark sample does not match product")
				}
			}
		}
		if len(sources) > 0 {
			validated = append(validated, map[string]any{"product_key": key, "technical_sources": sources})
		}
	}
	for _, row := range validated {
		product := products[row["product_key"].(string)]
		specs, _ := product["specifications"].(map[string]any)
		copy := map[string]any{}
		for k, v := range specs {
			copy[k] = v
		}
		copy["technical_sources"] = row["technical_sources"]
		product["specifications"] = copy
	}
	return validated, nil
}

// TechnicalEvidenceFresh bounds cache age using the collection time, not publication date.
func TechnicalEvidenceFresh(raw []map[string]any, now time.Time) bool {
	if len(raw) == 0 {
		return false
	}
	for _, row := range raw {
		sources, err := cleanTechnicalSources(row["technical_sources"])
		if err != nil || len(sources) == 0 {
			return false
		}
		for _, raw := range sources {
			s := raw.(map[string]any)
			stamp, _ := s["observed_at"].(string)
			seen, err := time.Parse(time.RFC3339, stamp)
			if err != nil || seen.After(now.Add(5*time.Minute)) || now.Sub(seen) > 7*24*time.Hour {
				return false
			}
		}
	}
	return true
}

// AttachBenchmarkComparisons compares only matching tests from the same table snapshot.
// It does not declare universal equivalence or change market-tier eligibility.
func AttachBenchmarkComparisons(result map[string]any) {
	reference, _ := result["product"].(map[string]any)
	items, _ := result["alternatives"].([]any)
	for _, raw := range items {
		candidate, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		delete(candidate, "benchmark_comparisons")
		comparisons := []any{}
		for _, leftRaw := range CollectedTechnicalSources(reference) {
			left := leftRaw.(map[string]any)
			conditions, _ := left["test_conditions"].(string)
			if conditions == "" {
				continue
			}
			for _, rightRaw := range CollectedTechnicalSources(candidate) {
				right := rightRaw.(map[string]any)
				if left["url"] != right["url"] || conditions != right["test_conditions"] {
					continue
				}
				a, _ := left["benchmarks"].([]any)
				b, _ := right["benchmarks"].([]any)
				for _, x := range a {
					one := x.(map[string]any)
					for _, y := range b {
						two := y.(map[string]any)
						if one["test"] != two["test"] || one["unit"] != two["unit"] || one["snapshot"] != two["snapshot"] || one["higher_is_better"] != two["higher_is_better"] {
							continue
						}
						before, _ := benchmarkValue(one["value"])
						after, _ := benchmarkValue(two["value"])
						difference := (after/before - 1) * 10000
						if math.IsNaN(difference) || math.IsInf(difference, 0) || math.Abs(difference) > 1e9 {
							continue
						}
						comparisons = append(comparisons, map[string]any{"test": one["test"], "unit": one["unit"], "reference_value": before, "candidate_value": after, "difference_basis_points": int64(math.Round(difference)), "higher_is_better": one["higher_is_better"], "url": left["url"], "observed_at": left["observed_at"], "published_at": left["published_at"], "test_conditions": conditions})
					}
				}
			}
		}
		if len(comparisons) > 0 {
			candidate["benchmark_comparisons"] = comparisons
		}
	}
}
