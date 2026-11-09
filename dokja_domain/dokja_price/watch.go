package price

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// maxWatches bounds the watched searches: each one is a store query on every collection.
const maxWatches = 50

// WatchOutcome is what a search did to the watched searches. Status is "added", "watched"
// (already followed), "full" (the limit was reached) or "not_owner" (only the owner grows the
// list; anyone else gets an answer from what is already known).
type WatchOutcome struct {
	Watch  Watch
	Status string
}

func (o WatchOutcome) Map() map[string]any {
	return map[string]any{"category": o.Watch.Category, "brand": o.Watch.Brand, "status": o.Status}
}

// watchOf normalizes a category and a brand the way the list keeps them.
func watchOf(category, brand string) Watch {
	return Watch{Category: strings.Join(words(category), " "), Brand: strings.Join(words(brand), " ")}
}

// mayGrowWatches is the business rule for who extends what the collectors follow: the owner, on
// Discord, or the operator at the Desktop, CLI or TUI on this machine.
func mayGrowWatches(event Event) bool {
	if owner, _ := event.Context["is_owner"].(bool); owner {
		return true
	}
	switch event.Source {
	case "desktop", "cli", "tui":
		return true
	}
	return false
}

// watchFromSearch follows a search's category and brand when the person may grow the list. A
// search without a category changes nothing: "everything from a brand" is not a store query.
func (d *Domain) watchFromSearch(ctx context.Context, event Event, search Search) (*WatchOutcome, error) {
	watch := watchOf(search.Query, search.Brand)
	if watch.Category == "" || search.Product != "" {
		return nil, nil
	}
	if !mayGrowWatches(event) {
		return &WatchOutcome{Watch: watch, Status: "not_owner"}, nil
	}
	watches, err := d.repository.Watches(ctx)
	if err != nil {
		return nil, err
	}
	for _, existing := range watches {
		if existing == watch {
			return &WatchOutcome{Watch: watch, Status: "watched"}, nil
		}
	}
	if len(watches) >= maxWatches {
		return &WatchOutcome{Watch: watch, Status: "full"}, nil
	}
	if _, err := d.repository.AddWatch(ctx, watch); err != nil {
		return nil, err
	}
	return &WatchOutcome{Watch: watch, Status: "added"}, nil
}

// unwatch forgets a watched search; only the owner may.
func (d *Domain) unwatch(ctx context.Context, event Event, category, brand string) (map[string]any, error) {
	if !mayGrowWatches(event) {
		return nil, fmt.Errorf("only the owner changes the watched searches")
	}
	watch := watchOf(category, brand)
	if watch.Category == "" {
		return nil, fmt.Errorf("category is required")
	}
	removed, err := d.repository.RemoveWatch(ctx, watch)
	if err != nil {
		return nil, err
	}
	return map[string]any{"category": watch.Category, "brand": watch.Brand, "removed": removed}, nil
}

// watchList is the watched searches, as collectors and interfaces read them.
func (d *Domain) watchList(ctx context.Context) (map[string]any, error) {
	watches, err := d.repository.Watches(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]any, 0, len(watches))
	for _, watch := range watches {
		rows = append(rows, map[string]any{"category": watch.Category, "brand": watch.Brand})
	}
	return map[string]any{"watches": rows, "count": len(rows), "limit": maxWatches}, nil
}

// Watches returns the watched searches, for the orchestrator to hand to a collector.
func (d *Domain) Watches(ctx context.Context) ([]Watch, error) {
	return d.repository.Watches(ctx)
}

// MissingMarket lists up to limit products that have no market-low history yet, oldest key
// first, so a collector can look each up once instead of on every run.
func (d *Domain) MissingMarket(ctx context.Context, limit int) ([]Product, error) {
	products, err := d.repository.Products(ctx)
	if err != nil {
		return nil, err
	}
	var missing []Product
	for _, product := range products {
		if len(missing) == limit {
			break
		}
		keys, err := d.repository.Keys(ctx, product.Key)
		if err != nil {
			return nil, err
		}
		market := false
		for _, key := range keys {
			market = market || key.Series == SeriesMarketMin
		}
		if !market {
			missing = append(missing, product)
		}
	}
	return missing, nil
}

// MarketPage is a product whose market page the collector already found: it can refresh the
// stores' offers and the market history from that page instead of searching for it again.
type MarketPage struct {
	Product Product
	// Page is the market page the collector gave with the market history.
	Page string
	// SeenOn is the newest market day known, so the stalest pages are refreshed first.
	SeenOn string
}

// MarketPages lists up to limit products with a known market page, the least recently seen
// first, so every product's offers are refreshed in turn even when a run cannot cover them all.
func (d *Domain) MarketPages(ctx context.Context, limit int) ([]MarketPage, error) {
	products, err := d.repository.Products(ctx)
	if err != nil {
		return nil, err
	}
	var pages []MarketPage
	for _, product := range products {
		urls, err := d.repository.OfferURLs(ctx, product.Key)
		if err != nil {
			return nil, err
		}
		for key, page := range urls {
			if key.Series != SeriesMarketMin || page == "" {
				continue
			}
			points, err := d.repository.History(ctx, key, "", "")
			if err != nil {
				return nil, err
			}
			seen := ""
			if len(points) > 0 {
				seen = points[len(points)-1].On
			}
			pages = append(pages, MarketPage{Product: product, Page: page, SeenOn: seen})
			break
		}
	}
	sort.SliceStable(pages, func(i, j int) bool {
		if pages[i].SeenOn != pages[j].SeenOn {
			return pages[i].SeenOn < pages[j].SeenOn
		}
		return pages[i].Product.Key < pages[j].Product.Key
	})
	if len(pages) > limit {
		pages = pages[:limit]
	}
	return pages, nil
}
