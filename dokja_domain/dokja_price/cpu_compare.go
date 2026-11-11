package price

import "strings"

// CPUBrand identifies the manufacturer from collector metadata or the product name.
func CPUBrand(brand, title string) string {
	brand = strings.ToLower(strings.TrimSpace(brand))
	for _, word := range strings.Fields(strings.ToLower(title)) {
		switch strings.Trim(word, "(),-®™") {
		case "amd", "ryzen":
			return "amd"
		case "intel":
			return "intel"
		}
	}
	return brand
}

func compareCPUs(original, candidate Product, reasons []string) (classification, confidence string, reasonCodes []string) {
	leftSocket, _ := stringSpec(original.Specifications, "socket")
	rightSocket, _ := stringSpec(candidate.Specifications, "socket")
	leftSocket = strings.Join(strings.Fields(strings.ToLower(leftSocket)), "")
	rightSocket = strings.Join(strings.Fields(strings.ToLower(rightSocket)), "")
	if leftSocket != "" && rightSocket != "" && leftSocket != rightSocket {
		reasons = append(reasons, "different_cpu_socket")
	}

	leftBenchmark, _ := stringSpec(original.Specifications, "benchmark")
	rightBenchmark, _ := stringSpec(candidate.Specifications, "benchmark")
	leftScore, _ := numericSpec(original.Specifications, "performance_score")
	rightScore, _ := numericSpec(candidate.Specifications, "performance_score")
	if leftBenchmark != "" && leftBenchmark == rightBenchmark && leftScore > 0 && rightScore > 0 {
		if rightScore > leftScore*1.10 {
			return "technically_superior", "medium", append(reasons, "shared_cpu_benchmark")
		}
		if rightScore >= leftScore*0.90 {
			return "technically_comparable", "medium", append(reasons, "shared_cpu_benchmark")
		}
		return "same_category_candidate", "low", append(reasons, "lower_cpu_benchmark")
	}

	leftCores, _ := numericSpec(original.Specifications, "cores")
	rightCores, _ := numericSpec(candidate.Specifications, "cores")
	leftThreads, _ := numericSpec(original.Specifications, "threads")
	rightThreads, _ := numericSpec(candidate.Specifications, "threads")
	if leftCores > 0 && leftCores == rightCores && leftThreads > 0 && leftThreads == rightThreads {
		return "similar_cpu_specs", "low", append(reasons, "same_cpu_cores_threads")
	}
	return "same_category_candidate", "low", reasons
}

// includeCompetingCPU keeps the highest-ranked competing brand visible if the first page
// would otherwise show only the original brand or products with an unknown brand.
func includeCompetingCPU(original Product, candidates []Alternative) []Alternative {
	brand := CPUBrand(original.Brand, original.Title)
	if brand == "" {
		return candidates
	}
	for i, alternative := range candidates {
		other := CPUBrand(alternative.Product.Brand, alternative.Product.Title)
		if other != "" && other != brand {
			if i >= maxCandidates {
				candidates[maxCandidates-1] = alternative
			}
			break
		}
	}
	return candidates
}
