package price

import "testing"

func TestDecideRejectsOfferAboveBudgetAndReportsAlternatives(t *testing.T) {
	decision := Decide(DecisionInput{
		Product: Product{Key: "pn:rx7600", Category: "placa de video"},
		Offers: []Offer{{Assessment: Assessment{
			Key:         Key{Product: "pn:rx7600", Merchant: "pichau", Series: SeriesMerchant, Kind: KindPix},
			Latest:      Point{Cents: 339999},
			HistoryDays: 2,
		}}},
		BudgetCents: 300000,
		Products: []Product{
			{Key: "pn:rx7600", Category: "placa de video"},
			{Key: "pn:rtx3060", Category: "placa de video"},
		},
	})
	if decision.Action != ActionSeekAlternative {
		t.Fatalf("action = %q", decision.Action)
	}
	if decision.SelectedOffer != "pichau" || decision.SelectedCents != 339999 {
		t.Fatalf("selected offer = %+v", decision)
	}
	if decision.Alternatives != 1 || decision.Confidence != "low" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestDecideBuysWithinBudgetAndDoesNotCountMarketReference(t *testing.T) {
	decision := Decide(DecisionInput{
		Product: Product{Key: "pn:ssd", Category: "ssd"},
		Offers: []Offer{
			{Assessment: Assessment{Key: Key{Product: "pn:ssd", Merchant: "kabum", Series: SeriesMerchant, Kind: KindPix}, Latest: Point{Cents: 95000}, HistoryDays: 120}},
			{Assessment: Assessment{Key: Key{Product: "pn:ssd", Merchant: "zoom", Series: SeriesMarketMin, Kind: KindUnknown}, Latest: Point{Cents: 80000}}},
		},
		BudgetCents: 100000,
	})
	if decision.Action != ActionBuyOriginal || decision.SelectedOffer != "kabum" {
		t.Fatalf("decision = %+v", decision)
	}
	if decision.Confidence != "high" {
		t.Fatalf("confidence = %q", decision.Confidence)
	}
}
