package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	pricedomain "read_books/dokja_domain/dokja_price"
	"read_books/internal/clients"
	"read_books/internal/core"
	"read_books/internal/logger"
)

// priceNotificationTTL bounds how long a "I will come back" price promise lives:
// an answer that arrives next month is not a follow-up anymore.
const priceNotificationTTL = 7 * 24 * time.Hour

// maxPriceNotifications bounds the pending registry; watches are few and a
// notification is a nicety, so a small cap keeps the snapshot honest.
const maxPriceNotifications = 20

// maxPriceNotificationProducts bounds how many products one follow-up lists.
const maxPriceNotificationProducts = 3

var priceNotificationPhrases = map[string]map[string]string{
	"pt": {
		"arrived":       "🔎 Chegou preço para **{search}**:",
		"arrived_title": "🔎 Chegou preço para {search}",
		"offer":         "• {title} — {merchant} R$ {price}",
		"discount":      " (de R$ {list})",
		"market":        " · mercado mín. R$ {price}",
		"verdict":       "Veredito completo: /preco produto:{search}",
		"expired":       "Tentei de novo **{search}** e as lojas ainda não trouxeram nada; parei de vigiar esse retorno.",
		"noPrices":      "Tentei de novo **{search}** e as lojas ainda não trouxeram nada. Continuo de olho e volto quando chegar.",
	},
	"en": {
		"arrived":       "🔎 Prices arrived for **{search}**:",
		"arrived_title": "🔎 Prices arrived for {search}",
		"offer":         "• {title} — {merchant} R$ {price}",
		"discount":      " (was R$ {list})",
		"market":        " · market low R$ {price}",
		"verdict":       "Full verdict: /price product:{search}",
		"expired":       "I retried **{search}** and the stores still brought nothing; I stopped watching for this return.",
		"noPrices":      "I retried **{search}** and the stores still brought nothing. I am still watching and will come back.",
	},
}

// pendingPriceNotification is one promised return for one watched search. It
// names where it came from; the search re-runs from the watch alone.
type pendingPriceNotification struct {
	ChannelID    string     `json:"channel_id"`
	ReplyToID    string     `json:"reply_to_id"`
	Category     string     `json:"category"`
	Brand        string     `json:"brand"`
	Search       string     `json:"search"`
	Event        core.Event `json:"event"`
	RegisteredAt time.Time  `json:"registered_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
}

func priceNotificationKey(channelID string, watch pricedomain.Watch) string {
	return channelID + "\x00" + watch.Category + "\x00" + watch.Brand
}

// PriceNotifier settles "come back when this watched search has prices" promises.
// The handler registers them when a new watch collects nothing; the intake fires
// them after a run records new observations. The registry mirrors to a store like
// ADR 0017's: a restart restores it and keeps watching.
type PriceNotifier struct {
	mu        sync.Mutex
	pending   map[string]pendingPriceNotification
	store     DeferredReadingStore
	deliverer ProactiveDeliverer
	domain    *pricedomain.Domain
	now       func() time.Time
	restore   sync.Once
}

func NewPriceNotifier(store DeferredReadingStore, deliverer ProactiveDeliverer, domain *pricedomain.Domain) *PriceNotifier {
	return &PriceNotifier{
		pending:   map[string]pendingPriceNotification{},
		store:     store,
		deliverer: deliverer,
		domain:    domain,
		now:       time.Now,
	}
}

func (n *PriceNotifier) persistLocked(ctx context.Context) {
	if n.store == nil {
		return
	}
	snapshot := make([]pendingPriceNotification, 0, len(n.pending))
	for _, entry := range n.pending {
		snapshot = append(snapshot, entry)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		logger.Info(fmt.Sprintf("price notifications snapshot failed: %v", err))
		return
	}
	if len(snapshot) == 0 {
		encoded = nil
	}
	if err := n.store.Save(ctx, encoded); err != nil {
		logger.Info(fmt.Sprintf("price notifications persistence failed: %v", err))
	}
}

func (n *PriceNotifier) restoreOnce(ctx context.Context) {
	n.restore.Do(func() {
		if n.store == nil {
			return
		}
		data, found, err := n.store.Load(ctx)
		if err != nil || !found {
			if err != nil {
				logger.Info(fmt.Sprintf("price notifications restore failed: %v", err))
			}
			return
		}
		var snapshot []pendingPriceNotification
		if err := json.Unmarshal(data, &snapshot); err != nil {
			logger.Info("price notifications snapshot was unreadable; forgotten")
			return
		}
		now := n.now()
		n.mu.Lock()
		for _, entry := range snapshot {
			if now.After(entry.ExpiresAt) {
				continue
			}
			n.pending[priceNotificationKey(entry.ChannelID, pricedomain.Watch{Category: entry.Category, Brand: entry.Brand})] = entry
		}
		count := len(n.pending)
		n.mu.Unlock()
		if count > 0 {
			logger.Info(fmt.Sprintf("price notifications restored count=%d", count))
		}
	})
}

// Register records one promise. It refuses duplicates, a full registry, or a
// setup without delivery; a promise the application cannot settle must not be made.
func (n *PriceNotifier) Register(ctx context.Context, event core.Event, watch pricedomain.Watch, search string) bool {
	if n == nil || n.deliverer == nil || n.domain == nil || watch.Category == "" {
		return false
	}
	n.restoreOnce(ctx)
	n.mu.Lock()
	defer n.mu.Unlock()
	now := n.now()
	for key, entry := range n.pending {
		if now.After(entry.ExpiresAt) {
			delete(n.pending, key)
			continue
		}
		if key == priceNotificationKey(event.Channel.ID, watch) {
			return false
		}
	}
	if len(n.pending) >= maxPriceNotifications {
		return false
	}
	n.pending[priceNotificationKey(event.Channel.ID, watch)] = pendingPriceNotification{
		ChannelID:    event.Channel.ID,
		ReplyToID:    resultString(event.Context, "message_id"),
		Category:     watch.Category,
		Brand:        watch.Brand,
		Search:       search,
		Event:        event,
		RegisteredAt: now,
		ExpiresAt:    now.Add(priceNotificationTTL),
	}
	n.persistLocked(ctx)
	return true
}

// NotifyWatches settles the promises whose watch just collected data. Searches
// that still return nothing keep their notification; everything else delivers
// once and forgets.
func (n *PriceNotifier) NotifyWatches(ctx context.Context, watches []pricedomain.Watch) {
	if n == nil || n.deliverer == nil || n.domain == nil {
		return
	}
	n.restoreOnce(ctx)
	for _, watch := range watches {
		n.notifyWatch(ctx, watch)
	}
}

func (n *PriceNotifier) notifyWatch(ctx context.Context, watch pricedomain.Watch) {
	n.mu.Lock()
	now := n.now()
	var entry pendingPriceNotification
	matched := false
	for key, pending := range n.pending {
		if now.After(pending.ExpiresAt) {
			delete(n.pending, key)
			continue
		}
		if pending.Category == watch.Category && pending.Brand == watch.Brand {
			entry, matched = pending, true
			break
		}
	}
	n.persistLocked(ctx)
	n.mu.Unlock()
	if !matched {
		return
	}
	result, err := n.domain.SearchProducts(ctx, pricedomain.Search{Query: entry.Category, Brand: entry.Brand})
	if err != nil || len(result.Products) == 0 {
		return // nothing to say yet; the next run tries again
	}
	embed, err := priceNotificationEmbed(entry.Event, entry.Search, result)
	if err != nil {
		return
	}
	if err := n.deliverer.DeliverReply(ctx, entry.ChannelID, "", entry.ReplyToID, []clients.DiscordDeliveryEmbed{embed}); err != nil {
		logger.Info(fmt.Sprintf("price notification delivery failed channel=%s watch=%s: %v", entry.ChannelID, entry.Category, err))
		return
	}
	n.mu.Lock()
	delete(n.pending, priceNotificationKey(entry.ChannelID, pricedomain.Watch{Category: entry.Category, Brand: entry.Brand}))
	n.persistLocked(ctx)
	n.mu.Unlock()
}

// ExpireDue delivers one fixed notice for promises past their TTL and drops them,
// so a channel is never left with a silent promise. The intake calls this each run.
func (n *PriceNotifier) ExpireDue(ctx context.Context) {
	if n == nil || n.deliverer == nil {
		return
	}
	n.restoreOnce(ctx)
	n.mu.Lock()
	now := n.now()
	var expired []pendingPriceNotification
	for key, entry := range n.pending {
		if now.After(entry.ExpiresAt) {
			expired = append(expired, entry)
			delete(n.pending, key)
		}
	}
	n.persistLocked(ctx)
	n.mu.Unlock()
	for _, entry := range expired {
		content := priceNotificationPhrase(entry.Event, "expired", entry.Search)
		if err := n.deliverer.DeliverReply(ctx, entry.ChannelID, content, entry.ReplyToID, nil); err != nil {
			logger.Info(fmt.Sprintf("price notification expiry delivery failed channel=%s: %v", entry.ChannelID, err))
		}
	}
}

// priceNotificationEmbed wraps the follow-up in the interface's embed shape: the
// header names the search, the description carries the offer lines, the footer
// points back to the /preco command.
func priceNotificationEmbed(event core.Event, search string, result pricedomain.SearchResult) (clients.DiscordDeliveryEmbed, error) {
	search = strings.TrimSpace(search)
	if search == "" {
		return clients.DiscordDeliveryEmbed{}, fmt.Errorf("empty search")
	}
	var lines []string
	for _, product := range result.Products {
		if len(lines) == maxPriceNotificationProducts {
			break
		}
		cheapest := cheapestMerchantOffer(product.Assessments)
		if cheapest == nil || cheapest.Latest.Cents <= 0 {
			continue
		}
		merchant := cheapest.Key.Merchant
		if merchant == "" {
			merchant = "market"
		}
		line := "• " + product.Product.Title + " — " + merchant + " R$ " + formatCents(cheapest.Latest.Cents)
		if cheapest.Latest.ListCents > cheapest.Latest.Cents {
			line += priceNotificationPhrase(event, "discount", search, "list", formatCents(cheapest.Latest.ListCents))
		}
		if market := cheapestMarketPoint(product.Market); market > 0 && market < cheapest.Latest.Cents {
			line += priceNotificationPhrase(event, "market", search, "price", formatCents(market))
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return clients.DiscordDeliveryEmbed{}, fmt.Errorf("no offers")
	}
	return clients.DiscordDeliveryEmbed{
		Title:       priceNotificationPhrase(event, "arrived_title", search),
		Description: strings.Join(lines, "\n"),
		Footer:      priceNotificationPhrase(event, "verdict", search),
		Color:       0x2b8a78,
	}, nil
}

// priceNotificationFollowup composes the fixed follow-up from the fresh search:
// the cheapest offer per matching product, then a pointer back to the command.
func priceNotificationFollowup(event core.Event, search string, result pricedomain.SearchResult) string {
	search = strings.TrimSpace(search)
	if search == "" {
		return ""
	}
	var lines []string
	for _, product := range result.Products {
		if len(lines) == maxPriceNotificationProducts {
			break
		}
		cheapest := cheapestMerchantOffer(product.Assessments)
		if cheapest == nil || cheapest.Latest.Cents <= 0 {
			continue
		}
		merchant := cheapest.Key.Merchant
		if merchant == "" {
			merchant = "market"
		}
		line := priceNotificationPhrase(event, "offer", search,
			"title", product.Product.Title, "merchant", merchant, "price", formatCents(cheapest.Latest.Cents))
		if cheapest.Latest.ListCents > cheapest.Latest.Cents {
			line += priceNotificationPhrase(event, "discount", search, "list", formatCents(cheapest.Latest.ListCents))
		}
		if market := cheapestMarketPoint(product.Market); market > 0 && market < cheapest.Latest.Cents {
			line += priceNotificationPhrase(event, "market", search, "price", formatCents(market))
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	header := priceNotificationPhrase(event, "arrived", search)
	footer := priceNotificationPhrase(event, "verdict", search)
	return header + "\n" + strings.Join(lines, "\n") + "\n" + footer
}

// cheapestMerchantOffer picks the cheapest store offer; the market aggregate is a
// reference, not a store to buy from, and never speaks for the product alone.
func cheapestMerchantOffer(assessments []pricedomain.Assessment) *pricedomain.Assessment {
	var best *pricedomain.Assessment
	for i := range assessments {
		if assessments[i].Key.Series == pricedomain.SeriesMarketMin || assessments[i].Latest.Cents <= 0 {
			continue
		}
		if best == nil || assessments[i].Latest.Cents < best.Latest.Cents {
			best = &assessments[i]
		}
	}
	return best
}

func cheapestMarketPoint(points []pricedomain.Point) int64 {
	var best int64
	for _, point := range points {
		if point.Cents > 0 && (best == 0 || point.Cents < best) {
			best = point.Cents
		}
	}
	return best
}

func priceNotificationPhrase(event core.Event, key, search string, pairs ...string) string {
	locale, _ := event.Payload["locale"].(string)
	if !strings.HasPrefix(locale, "en") {
		locale = "pt"
	} else {
		locale = "en"
	}
	text := priceNotificationPhrases[locale][key]
	replacements := []string{"{search}", search}
	for i := 0; i+1 < len(pairs); i += 2 {
		replacements = append(replacements, "{"+pairs[i]+"}", pairs[i+1])
	}
	return strings.NewReplacer(replacements...).Replace(text)
}

// formatCents renders a price in reais the way the price interfaces do:
// thousands separated by a dot, cents by a comma.
func formatCents(cents int64) string {
	reais, rest := cents/100, cents%100
	digits := fmt.Sprintf("%d", reais)
	if reais >= 1000 {
		head := digits[:len(digits)-3]
		digits = head + "." + digits[len(digits)-3:]
	}
	return fmt.Sprintf("%s,%02d", digits, rest)
}
