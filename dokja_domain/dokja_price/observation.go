// Package price keeps the price history of the products the owner watches and decides
// whether a price is a real promotion.
//
// The domain knows no store. Collectors are owner plugins outside the repository; they
// describe what they saw with the price.observation.v1 contract, carried in a feed item's
// body. The domain validates that contract, keeps one point per product, merchant, series,
// price kind and day, and judges the current price against that history.
package price

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ContractVersion names the only observation shape this domain reads.
const ContractVersion = "price.observation.v1"

const (
	dateLayout = "2006-01-02"

	// maxPoints holds a little over a year of daily points, which is what aggregators
	// publish as history.
	maxPoints = 400
	maxTitle  = 300
	maxName   = 80
	// maxCents is ten million reais, far above any storage device.
	maxCents = int64(1_000_000_000)
	// maxBodyBytes matches the size Feeds allows for an item body.
	maxBodyBytes = 100_000
	// maxDecimals is how many decimals an amount may carry, which covers the noise of a
	// float64 printed in full; the first roundingDecimals decide the rounding to cents.
	maxDecimals      = 15
	roundingDecimals = 6
)

// Series says what a list of points measures. Points of different series are never
// compared: one store's price and the lowest price of a whole market move differently.
type Series string

const (
	// SeriesMerchant is one merchant's own price.
	SeriesMerchant Series = "merchant"
	// SeriesMarketMin is the lowest price among many merchants on each day, as price
	// aggregators publish it. The merchant of each point may change from day to day.
	SeriesMarketMin Series = "market_min"
)

// Kind is how the price is paid. A cash (PIX) price is usually lower than the card
// price of the same offer, so the two are separate histories.
type Kind string

const (
	KindPix     Kind = "pix"
	KindCard    Kind = "card"
	KindUnknown Kind = "unknown"
)

// Point is one day of a history. Prices are in cents. ListCents is the struck-through
// "from" price the merchant advertised for this offer, or zero when none was shown. It is
// never the price of the other payment kind: a card price next to a lower cash price is
// a separate history, not a list price, or every cash price would look discounted.
type Point struct {
	On        string
	Cents     int64
	ListCents int64
	// Seller is the merchant that set a market_min point, when the source says. It is
	// opaque and only informative.
	Seller string
}

// Observation is what one collector saw for one product at one merchant, in one series
// and price kind. A daily snapshot has one point; a backfill has many.
type Observation struct {
	Product      string
	Title        string
	Merchant     string
	MerchantName string
	Series       Series
	Kind         Kind
	Points       []Point
}

// Key identifies one history.
type Key struct {
	Product  string
	Merchant string
	Series   Series
	Kind     Kind
}

// Key returns the history this observation belongs to.
func (o Observation) Key() Key {
	return Key{Product: o.Product, Merchant: o.Merchant, Series: o.Series, Kind: o.Kind}
}

// ErrNotObservation means the body does not claim to be a price observation at all, so
// the item belongs to another kind of source. A body that claims the contract and breaks
// it is a different, reported error.
var ErrNotObservation = errors.New("not a price observation")

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,79}$`)

type wirePoint struct {
	On        string      `json:"on"`
	Price     json.Number `json:"price"`
	ListPrice json.Number `json:"list_price,omitempty"`
	Seller    string      `json:"seller,omitempty"`
}

type wireObservation struct {
	Contract     string      `json:"contract"`
	Product      string      `json:"product"`
	Title        string      `json:"title"`
	Merchant     string      `json:"merchant"`
	MerchantName string      `json:"merchant_name,omitempty"`
	Series       Series      `json:"series"`
	PriceKind    Kind        `json:"price_kind"`
	Currency     string      `json:"currency"`
	Points       []wirePoint `json:"points"`
}

// ParseObservation reads one price.observation.v1 body. today bounds the dates: a point
// may not lie after the collector's next day, which absorbs time zones.
func ParseObservation(body string, today time.Time) (Observation, error) {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "{") || !strings.Contains(trimmed, ContractVersion) {
		return Observation{}, ErrNotObservation
	}
	if len(trimmed) > maxBodyBytes {
		return Observation{}, fmt.Errorf("observation is larger than %d bytes", maxBodyBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(trimmed)))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var wire wireObservation
	if err := decoder.Decode(&wire); err != nil {
		return Observation{}, fmt.Errorf("observation is not valid JSON for %s: %w", ContractVersion, err)
	}
	if decoder.More() {
		return Observation{}, fmt.Errorf("observation has data after the JSON object")
	}
	if wire.Contract != ContractVersion {
		return Observation{}, ErrNotObservation
	}
	return wire.validate(today)
}

func (w wireObservation) validate(today time.Time) (Observation, error) {
	if !keyPattern.MatchString(w.Product) {
		return Observation{}, fmt.Errorf("product must be 1 to 80 lowercase letters, digits, '.', '_', ':' or '-'")
	}
	if !keyPattern.MatchString(w.Merchant) {
		return Observation{}, fmt.Errorf("merchant must be 1 to 80 lowercase letters, digits, '.', '_', ':' or '-'")
	}
	if w.Series != SeriesMerchant && w.Series != SeriesMarketMin {
		return Observation{}, fmt.Errorf("series must be %q or %q", SeriesMerchant, SeriesMarketMin)
	}
	if w.PriceKind != KindPix && w.PriceKind != KindCard && w.PriceKind != KindUnknown {
		return Observation{}, fmt.Errorf("price_kind must be %q, %q or %q", KindPix, KindCard, KindUnknown)
	}
	if w.Currency != "BRL" {
		return Observation{}, fmt.Errorf("currency must be BRL")
	}
	title, err := cleanText(w.Title, maxTitle)
	if err != nil || title == "" {
		return Observation{}, fmt.Errorf("title is required plain text")
	}
	name, err := cleanText(w.MerchantName, maxName)
	if err != nil {
		return Observation{}, fmt.Errorf("merchant_name must be plain text")
	}
	if len(w.Points) == 0 || len(w.Points) > maxPoints {
		return Observation{}, fmt.Errorf("points must hold 1 to %d entries", maxPoints)
	}
	latest := today.UTC().AddDate(0, 0, 1).Format(dateLayout)
	points := make([]Point, 0, len(w.Points))
	seen := make(map[string]bool, len(w.Points))
	for index, raw := range w.Points {
		point, err := raw.validate(latest)
		if err != nil {
			return Observation{}, fmt.Errorf("point %d: %w", index, err)
		}
		if seen[point.On] {
			return Observation{}, fmt.Errorf("point %d: date %s appears twice", index, point.On)
		}
		seen[point.On] = true
		points = append(points, point)
	}
	return Observation{
		Product: w.Product, Title: title, Merchant: w.Merchant, MerchantName: name,
		Series: w.Series, Kind: w.PriceKind, Points: points,
	}, nil
}

func (w wirePoint) validate(latest string) (Point, error) {
	on, err := time.Parse(dateLayout, w.On)
	if err != nil || on.Format(dateLayout) != w.On {
		return Point{}, fmt.Errorf("on must be a date as YYYY-MM-DD")
	}
	if w.On > latest {
		return Point{}, fmt.Errorf("date %s is in the future", w.On)
	}
	cents, err := toCents(w.Price)
	if err != nil || cents <= 0 {
		return Point{}, fmt.Errorf("price must be a positive amount in reais with at most 15 decimals")
	}
	var list int64
	if w.ListPrice != "" {
		list, err = toCents(w.ListPrice)
		if err != nil || list < 0 {
			return Point{}, fmt.Errorf("list_price must be an amount in reais with at most 15 decimals")
		}
	}
	seller, err := cleanText(w.Seller, maxName)
	if err != nil {
		return Point{}, fmt.Errorf("seller must be plain text")
	}
	return Point{On: w.On, Cents: cents, ListCents: list, Seller: seller}, nil
}

// toCents reads a decimal amount without going through floating point. Sources that
// compute prices in floating point print noise such as 1665.6100000000001, so up to 15
// decimals are accepted and the amount is rounded half up to the cent.
func toCents(number json.Number) (int64, error) {
	text := string(number)
	if strings.ContainsAny(text, "eE+-") {
		return 0, fmt.Errorf("amount %q is not a plain decimal", text)
	}
	whole, fraction, _ := strings.Cut(text, ".")
	if whole == "" || len(fraction) > maxDecimals {
		return 0, fmt.Errorf("amount %q is not a plain decimal", text)
	}
	fraction = (fraction + strings.Repeat("0", roundingDecimals))[:roundingDecimals]
	reais, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || reais > maxCents/100 {
		return 0, fmt.Errorf("amount %q is out of range", text)
	}
	micros, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q is not a plain decimal", text)
	}
	total := reais*100 + (micros+5_000)/10_000
	if total > maxCents {
		return 0, fmt.Errorf("amount %q is out of range", text)
	}
	return total, nil
}

// cleanText keeps one line of printable UTF-8 and cuts it to limit characters.
func cleanText(text string, limit int) (string, error) {
	if !utf8.ValidString(text) {
		return "", fmt.Errorf("text is not valid UTF-8")
	}
	var builder strings.Builder
	count := 0
	for _, r := range strings.TrimSpace(text) {
		if r < 0x20 || r == 0x7f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			r = ' '
		}
		if count == limit {
			break
		}
		builder.WriteRune(r)
		count++
	}
	return strings.Join(strings.Fields(builder.String()), " "), nil
}
