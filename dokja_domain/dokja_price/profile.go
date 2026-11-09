package price

// AttributeSpec describes a category attribute without claiming that a product has it.
// Collectors may provide these values later; missing values stay unknown.
type AttributeSpec struct {
	Key            string
	Required       bool
	HigherIsBetter bool
}

// CategoryProfile defines which objective attributes are useful for a category.
type CategoryProfile struct {
	ID         string
	Attributes []AttributeSpec
	Generic    bool
}

var categoryProfiles = map[string]CategoryProfile{
	"gpu": {
		ID: "gpu",
		Attributes: []AttributeSpec{
			{Key: "vram_gb", HigherIsBetter: true},
			{Key: "performance_score", HigherIsBetter: true},
			{Key: "power_w", HigherIsBetter: false},
			{Key: "ray_tracing", HigherIsBetter: true},
		},
	},
	"cpu": {
		ID: "cpu",
		Attributes: []AttributeSpec{
			{Key: "cores", HigherIsBetter: true},
			{Key: "threads", HigherIsBetter: true},
			{Key: "performance_score", HigherIsBetter: true},
			{Key: "power_w", HigherIsBetter: false},
			{Key: "socket", Required: true},
		},
	},
	"storage": {
		ID: "storage",
		Attributes: []AttributeSpec{
			{Key: "capacity_gb", HigherIsBetter: true},
			{Key: "read_mbps", HigherIsBetter: true},
			{Key: "write_mbps", HigherIsBetter: true},
			{Key: "interface", Required: true},
		},
	},
	"mobile": {
		ID: "mobile",
		Attributes: []AttributeSpec{
			{Key: "battery_mah", HigherIsBetter: true},
			{Key: "screen_hz", HigherIsBetter: true},
			{Key: "storage_gb", HigherIsBetter: true},
			{Key: "camera_mp", HigherIsBetter: true},
		},
	},
	"display": {
		ID: "display",
		Attributes: []AttributeSpec{
			{Key: "resolution"},
			{Key: "screen_hz", HigherIsBetter: true},
			{Key: "response_ms", HigherIsBetter: false},
			{Key: "panel"},
		},
	},
	"generic": {ID: "generic", Generic: true},
}

// CategoryProfileFor returns the most specific known profile, or a generic profile.
func CategoryProfileFor(category string) CategoryProfile {
	token := categoryToken(category)
	if profile, ok := categoryProfiles[token]; ok {
		return profile
	}
	return categoryProfiles["generic"]
}

// CandidateConfidence is deliberately low until comparable specifications are present.
func CandidateConfidence(product Product) string {
	_ = product
	return "low"
}
