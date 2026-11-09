package price

// Alternative is a current, auditable candidate found in the existing price history.
// It is not called equivalent unless comparable specifications are available.
type Alternative struct {
	Product          Product
	Offer            Offer
	Classification   string
	ReasonCodes      []string
	Confidence       string
	SharedAttributes int
}

func (a Alternative) Map() map[string]any {
	return map[string]any{
		"product":           a.Product.Key,
		"title":             a.Product.Title,
		"category":          a.Product.Category,
		"brand":             a.Product.Brand,
		"part_number":       a.Product.PartNumber,
		"merchant":          a.Offer.Assessment.Key.Merchant,
		"price_cents":       a.Offer.Assessment.Latest.Cents,
		"url":               a.Offer.URL,
		"classification":    a.Classification,
		"reason_codes":      append([]string(nil), a.ReasonCodes...),
		"confidence":        a.Confidence,
		"shared_attributes": a.SharedAttributes,
	}
}
