package price

import (
	"sort"
	"time"
)

// Verdict is the domain's judgement of the latest price of one history. Interfaces
// translate it; the domain returns no prose.
type Verdict string

const (
	// VerdictInsufficientHistory: too few earlier days to judge.
	VerdictInsufficientHistory Verdict = "insufficient_history"
	// VerdictStale: the latest point is more than staleAfterDays older than the day being judged.
	VerdictStale Verdict = "stale"
	// VerdictRealDiscount: clearly below what this history usually costs.
	VerdictRealDiscount Verdict = "real_discount"
	// VerdictInflatedBeforeSale: the price was raised in the days before and the "drop"
	// only brings it back to its usual level.
	VerdictInflatedBeforeSale Verdict = "inflated_before_sale"
	// VerdictFakeDiscount: the merchant advertises a discount the history does not show.
	VerdictFakeDiscount Verdict = "fake_discount"
	// VerdictNoDiscount: an ordinary price.
	VerdictNoDiscount Verdict = "no_discount"
)

// Signal is an extra fact about the latest price, independent of the verdict.
type Signal string

const (
	// SignalAllTimeLow: no earlier point of this history is lower.
	SignalAllTimeLow Signal = "all_time_low"
	// SignalListAboveObservedMax: the advertised "from" price is above anything this
	// history was actually sold for in the last 90 days.
	SignalListAboveObservedMax Signal = "list_above_observed_max"
	// SignalRaisedBeforeDrop: the price rose shortly before the latest point.
	SignalRaisedBeforeDrop Signal = "raised_before_drop"
)

// The thresholds below are in basis points (1/100 of a percent) so the arithmetic stays
// in integers.
const (
	// staleAfterDays is how old the latest point may be. Aggregators publish only closed days,
	// so their latest point is yesterday in the collector's time zone, which is already two
	// days before the UTC date late in a Brazilian evening.
	staleAfterDays = 2
	// minHistoryDays is how many earlier days a judgement needs.
	minHistoryDays = 7
	// realDiscountBps: the price must be at least 5% under the 30-day median.
	realDiscountBps = 500
	// advertisedDiscountBps: an advertised discount under 10% is not worth calling fake.
	advertisedDiscountBps = 1000
	// listToleranceBps absorbs rounding between the list price and observed prices.
	listToleranceBps = 200
	// markupBps: a recent peak 10% over the earlier median counts as a markup.
	markupBps = 1000
	// returnToNormalBps: after a markup, a price within 5% of the earlier median is only
	// the usual price again.
	returnToNormalBps = 500
	// markupWindowDays is how far back a markup before the sale is looked for.
	markupWindowDays = 14
	// normalWindowDays is how far back, beyond the markup window, the usual price is read.
	normalWindowDays = 60
)

// Assessment is the judgement of the latest point of one history.
type Assessment struct {
	Key     Key
	Verdict Verdict
	Signals []Signal
	Latest  Point
	// Median30Cents and Median90Cents are medians of the earlier points in those
	// windows; zero when the window is empty.
	Median30Cents int64
	Median90Cents int64
	Max90Cents    int64
	// LowestCents is the lowest earlier point of the whole history.
	LowestCents int64
	// DiscountVs30Bps is how far the latest price is under the 30-day median, in basis
	// points; negative when it is above.
	DiscountVs30Bps int64
	// AdvertisedDiscountBps is the discount the merchant shows against its list price.
	AdvertisedDiscountBps int64
	// HistoryDays counts the earlier points the judgement used.
	HistoryDays int
}

// Assess judges the latest point of a history, oldest first, as of today. Points of one
// history only: the caller never mixes series or price kinds.
func Assess(key Key, points []Point, today time.Time) Assessment {
	assessment := Assessment{Key: key, Verdict: VerdictInsufficientHistory}
	if len(points) == 0 {
		return assessment
	}
	latest := points[len(points)-1]
	earlier := points[:len(points)-1]
	assessment.Latest = latest
	assessment.HistoryDays = len(earlier)
	assessment.AdvertisedDiscountBps = discountBps(latest.ListCents, latest.Cents)

	latestDay, _ := time.Parse(dateLayout, latest.On)
	if today.UTC().Format(dateLayout) > latestDay.AddDate(0, 0, staleAfterDays).Format(dateLayout) {
		assessment.Verdict = VerdictStale
		return assessment
	}

	window := func(fromDays, toDays int) []int64 {
		from := latestDay.AddDate(0, 0, -fromDays).Format(dateLayout)
		to := latestDay.AddDate(0, 0, -toDays).Format(dateLayout)
		var cents []int64
		for _, point := range earlier {
			if point.On >= from && point.On <= to {
				cents = append(cents, point.Cents)
			}
		}
		return cents
	}
	last30 := window(30, 1)
	last90 := window(90, 1)
	assessment.Median30Cents = median(last30)
	assessment.Median90Cents = median(last90)
	assessment.Max90Cents = maximum(last90)
	if len(earlier) > 0 {
		lowest := earlier[0].Cents
		for _, point := range earlier {
			lowest = min(lowest, point.Cents)
		}
		assessment.LowestCents = lowest
		if latest.Cents <= lowest {
			assessment.Signals = append(assessment.Signals, SignalAllTimeLow)
		}
	}
	if assessment.Max90Cents > 0 && latest.ListCents > scale(assessment.Max90Cents, 10_000+listToleranceBps) {
		assessment.Signals = append(assessment.Signals, SignalListAboveObservedMax)
	}

	if len(last30) < minHistoryDays {
		return assessment
	}
	assessment.DiscountVs30Bps = discountBps(assessment.Median30Cents, latest.Cents)

	normal := median(window(markupWindowDays+normalWindowDays, markupWindowDays+1))
	recentPeak := maximum(window(markupWindowDays, 1))
	raised := normal > 0 && recentPeak > scale(normal, 10_000+markupBps)
	if raised {
		assessment.Signals = append(assessment.Signals, SignalRaisedBeforeDrop)
	}

	switch {
	case raised && latest.Cents < recentPeak && latest.Cents >= scale(normal, 10_000-returnToNormalBps):
		assessment.Verdict = VerdictInflatedBeforeSale
	case assessment.DiscountVs30Bps >= realDiscountBps &&
		(assessment.Median90Cents == 0 || discountBps(assessment.Median90Cents, latest.Cents) >= realDiscountBps):
		assessment.Verdict = VerdictRealDiscount
	case assessment.AdvertisedDiscountBps >= advertisedDiscountBps:
		assessment.Verdict = VerdictFakeDiscount
	default:
		assessment.Verdict = VerdictNoDiscount
	}
	return assessment
}

// discountBps is how far price is under reference, in basis points.
func discountBps(reference, price int64) int64 {
	if reference <= 0 || price <= 0 {
		return 0
	}
	return (reference - price) * 10_000 / reference
}

func scale(cents, bps int64) int64 { return cents * bps / 10_000 }

func median(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func maximum(values []int64) int64 {
	var top int64
	for _, value := range values {
		top = max(top, value)
	}
	return top
}
