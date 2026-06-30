// Package when parses fuzzy human time phrases ("last spring", "around march",
// "the week of jun 3", "2 years ago") into a concrete [start, end] window.
//
// The parser is intentionally small and self-contained: it recognizes a fixed
// grammar of common phrasings rather than attempting full natural-language
// understanding. Keeping it isolated (and resolving everything relative to an
// injectable "now") makes it easy to grow and to test with golden fixtures.
package when

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Window is a half-open time interval [Start, End) that a fuzzy time phrase
// resolves to. back-then ranks files by their proximity to this window.
type Window struct {
	Start time.Time
	End   time.Time
}

// Mid returns the midpoint of the window, used by ranking as the reference
// point a file's timestamp is measured against.
func (w Window) Mid() time.Time {
	return w.Start.Add(w.End.Sub(w.Start) / 2)
}

// Contains reports whether t falls within the half-open window [Start, End).
func (w Window) Contains(t time.Time) bool {
	return !t.Before(w.Start) && t.Before(w.End)
}

// Parse resolves a fuzzy time phrase into a Window relative to now. Parsing is
// case-insensitive and tolerant of surrounding whitespace and filler words
// like "around" or "the".
//
// Recognized forms (non-exhaustive examples):
//
//	"today", "yesterday"
//	"this week", "last week", "this month", "last month", "this year", "last year"
//	"N days/weeks/months/years ago" (e.g. "2 years ago", "3 weeks ago")
//	"last <month>", "this <month>", "<month>" (e.g. "last march", "december")
//	"<month> <year>" (e.g. "march 2024")
//	"<season>", "last <season>", "this <season>" (spring/summer/fall|autumn/winter)
//	"<year>" (e.g. "2018")
//	explicit dates: "2024-03-15", "2024-03", "2024/03/15"
//
// An empty or unrecognizable phrase returns an error so callers can prompt the
// user rather than silently returning the whole index.
func Parse(phrase string, now time.Time) (Window, error) {
	s := normalize(phrase)
	if s == "" {
		return Window{}, fmt.Errorf("empty time phrase")
	}

	for _, p := range parsers {
		if w, ok := p(s, now); ok {
			return w, nil
		}
	}
	return Window{}, fmt.Errorf("could not understand time phrase %q", phrase)
}

// normalize lowercases the phrase, collapses whitespace, and strips common
// filler words that don't change the resolved window ("around", "the", "of",
// "sometime", "in"). Leading/trailing punctuation is trimmed too.
func normalize(phrase string) string {
	s := strings.ToLower(strings.TrimSpace(phrase))
	s = strings.Trim(s, ".,!?")
	// Collapse runs of whitespace to single spaces.
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}

	filler := map[string]struct{}{
		"around": {}, "round": {}, "about": {}, "the": {},
		"sometime": {}, "somewhere": {}, "roughly": {}, "circa": {},
	}
	// "of" and "in" are only filler when not part of a date; dropping them is
	// safe for our grammar because no recognized form depends on them.
	filler["of"] = struct{}{}
	filler["in"] = struct{}{}

	var kept []string
	for _, tok := range strings.Fields(s) {
		if _, drop := filler[tok]; drop {
			continue
		}
		kept = append(kept, tok)
	}
	return strings.Join(kept, " ")
}

// parser attempts to resolve a normalized phrase. ok is false when the parser
// does not recognize the phrase, letting Parse try the next one.
type parser func(s string, now time.Time) (Window, bool)

// parsers is the ordered list of recognizers. Order matters: more specific
// forms (explicit dates, "N units ago") come before broad fallbacks (bare
// month, bare year) so a phrase is matched by the most precise parser.
var parsers = []parser{
	parseToday,
	parseRelativePeriod, // this/last week|month|year
	parseAgo,            // N days/weeks/months/years ago
	parseISODate,        // 2024-03-15 / 2024-03 / 2024
	parseMonthYear,      // march 2024
	parseSeason,         // (last|this) spring|summer|fall|winter
	parseMonth,          // (last|this) march / bare march
	parseYear,           // bare 2018
}

func dayWindow(t time.Time) Window {
	start := startOfDay(t)
	return Window{Start: start, End: start.AddDate(0, 0, 1)}
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func parseToday(s string, now time.Time) (Window, bool) {
	switch s {
	case "today", "now":
		return dayWindow(now), true
	case "yesterday":
		return dayWindow(now.AddDate(0, 0, -1)), true
	case "tomorrow":
		return dayWindow(now.AddDate(0, 0, 1)), true
	}
	return Window{}, false
}

// parseRelativePeriod handles "this week", "last week", "this month",
// "last month", "this year", "last year".
func parseRelativePeriod(s string, now time.Time) (Window, bool) {
	parts := strings.Fields(s)
	if len(parts) != 2 {
		return Window{}, false
	}
	rel := parts[0]
	unit := parts[1]
	var back int
	switch rel {
	case "this":
		back = 0
	case "last", "past", "previous":
		back = 1
	default:
		return Window{}, false
	}

	switch unit {
	case "week", "weeks":
		start := startOfWeek(now).AddDate(0, 0, -7*back)
		return Window{Start: start, End: start.AddDate(0, 0, 7)}, true
	case "month", "months":
		y, m := now.Year(), now.Month()
		start := time.Date(y, m, 1, 0, 0, 0, 0, now.Location()).AddDate(0, -back, 0)
		return Window{Start: start, End: start.AddDate(0, 1, 0)}, true
	case "year", "years":
		start := time.Date(now.Year()-back, 1, 1, 0, 0, 0, 0, now.Location())
		return Window{Start: start, End: start.AddDate(1, 0, 0)}, true
	}
	return Window{}, false
}

// startOfWeek returns midnight on the Monday of t's week.
func startOfWeek(t time.Time) time.Time {
	d := startOfDay(t)
	// Go's Weekday: Sunday=0..Saturday=6. We want Monday as the first day.
	offset := (int(d.Weekday()) + 6) % 7
	return d.AddDate(0, 0, -offset)
}

var agoRe = regexp.MustCompile(`^(\d+) (day|days|week|weeks|month|months|year|years) ago$`)

// parseAgo handles "N days/weeks/months/years ago". The resulting window is a
// span around the target time whose width scales with the unit, reflecting the
// fuzziness of the phrase (a "year ago" is vaguer than "3 days ago").
func parseAgo(s string, now time.Time) (Window, bool) {
	m := agoRe.FindStringSubmatch(s)
	if m == nil {
		return Window{}, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return Window{}, false
	}
	unit := strings.TrimSuffix(m[2], "s")

	switch unit {
	case "day":
		// Exact day, N days back.
		return dayWindow(now.AddDate(0, 0, -n)), true
	case "week":
		// The 7-day week N weeks back, centered on that span.
		center := now.AddDate(0, 0, -7*n)
		start := startOfWeek(center)
		return Window{Start: start, End: start.AddDate(0, 0, 7)}, true
	case "month":
		target := now.AddDate(0, -n, 0)
		start := time.Date(target.Year(), target.Month(), 1, 0, 0, 0, 0, now.Location())
		return Window{Start: start, End: start.AddDate(0, 1, 0)}, true
	case "year":
		target := now.AddDate(-n, 0, 0)
		start := time.Date(target.Year(), 1, 1, 0, 0, 0, 0, now.Location())
		return Window{Start: start, End: start.AddDate(1, 0, 0)}, true
	}
	return Window{}, false
}

var isoRe = regexp.MustCompile(`^(\d{4})([-/](\d{1,2})([-/](\d{1,2}))?)?$`)

// parseISODate handles explicit "2024-03-15", "2024/03", and "2024-03" forms.
// A bare 4-digit year is left to parseYear (this parser requires a separator
// for month/day precision but also accepts the year-only case to keep dates
// and years consistent).
func parseISODate(s string, now time.Time) (Window, bool) {
	m := isoRe.FindStringSubmatch(s)
	if m == nil {
		return Window{}, false
	}
	year, _ := strconv.Atoi(m[1])
	// Year only: defer to parseYear's semantics by returning the whole year.
	if m[3] == "" {
		start := time.Date(year, 1, 1, 0, 0, 0, 0, now.Location())
		return Window{Start: start, End: start.AddDate(1, 0, 0)}, true
	}
	month, _ := strconv.Atoi(m[3])
	if month < 1 || month > 12 {
		return Window{}, false
	}
	if m[5] == "" {
		start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, now.Location())
		return Window{Start: start, End: start.AddDate(0, 1, 0)}, true
	}
	day, _ := strconv.Atoi(m[5])
	if day < 1 || day > 31 {
		return Window{}, false
	}
	start := time.Date(year, time.Month(month), day, 0, 0, 0, 0, now.Location())
	return Window{Start: start, End: start.AddDate(0, 0, 1)}, true
}

// months maps recognized month names and common abbreviations to their number.
var months = map[string]time.Month{
	"january": time.January, "jan": time.January,
	"february": time.February, "feb": time.February,
	"march": time.March, "mar": time.March,
	"april": time.April, "apr": time.April,
	"may":  time.May,
	"june": time.June, "jun": time.June,
	"july": time.July, "jul": time.July,
	"august": time.August, "aug": time.August,
	"september": time.September, "sep": time.September, "sept": time.September,
	"october": time.October, "oct": time.October,
	"november": time.November, "nov": time.November,
	"december": time.December, "dec": time.December,
}

var monthYearRe = regexp.MustCompile(`^([a-z]+) (\d{4})$`)

// parseMonthYear handles "march 2024" and "mar 2024".
func parseMonthYear(s string, now time.Time) (Window, bool) {
	m := monthYearRe.FindStringSubmatch(s)
	if m == nil {
		return Window{}, false
	}
	mon, ok := months[m[1]]
	if !ok {
		return Window{}, false
	}
	year, _ := strconv.Atoi(m[2])
	start := time.Date(year, mon, 1, 0, 0, 0, 0, now.Location())
	return Window{Start: start, End: start.AddDate(0, 1, 0)}, true
}

// parseMonth handles "(last|this) <month>" and a bare "<month>". For a bare or
// "last" month, the most recent occurrence at or before now is chosen so that
// "december" in March 2025 means December 2024, not the upcoming December.
func parseMonth(s string, now time.Time) (Window, bool) {
	parts := strings.Fields(s)
	var rel string
	var name string
	switch len(parts) {
	case 1:
		name = parts[0]
	case 2:
		rel, name = parts[0], parts[1]
	default:
		return Window{}, false
	}

	mon, ok := months[name]
	if !ok {
		return Window{}, false
	}

	year := now.Year()
	switch rel {
	case "", "last", "past":
		// Most recent occurrence at or before the current month.
		if mon > now.Month() {
			year--
		}
		// "last march" while in march means this march; only step back a year
		// when the month hasn't occurred yet this year. That matches intuition
		// for both bare and "last" usage.
	case "this":
		// This calendar year's occurrence.
	case "next", "coming":
		if mon < now.Month() {
			year++
		}
	default:
		return Window{}, false
	}

	start := time.Date(year, mon, 1, 0, 0, 0, 0, now.Location())
	return Window{Start: start, End: start.AddDate(0, 1, 0)}, true
}

// seasonMonths maps a season to its starting month (Northern Hemisphere
// meteorological seasons: spring=Mar, summer=Jun, fall=Sep, winter=Dec).
var seasonMonths = map[string]time.Month{
	"spring": time.March,
	"summer": time.June,
	"fall":   time.September,
	"autumn": time.September,
	"winter": time.December,
}

// parseSeason handles "(last|this) <season>" and a bare "<season>". A season is
// a three-month span. Winter spans the year boundary (Dec–Feb); we anchor it on
// its December. As with months, a bare/"last" season resolves to the most
// recent completed-or-current occurrence.
func parseSeason(s string, now time.Time) (Window, bool) {
	parts := strings.Fields(s)
	var rel, name string
	switch len(parts) {
	case 1:
		name = parts[0]
	case 2:
		rel, name = parts[0], parts[1]
	default:
		return Window{}, false
	}

	startMon, ok := seasonMonths[name]
	if !ok {
		return Window{}, false
	}

	year := now.Year()
	switch rel {
	case "", "last", "past":
		// Pick the most recent occurrence at or before now. The season's start
		// month decides whether this year's instance has begun yet.
		if startMon > now.Month() {
			year--
		}
		if rel == "last" {
			// "last spring" should mean the previous one even if we're mid-season.
			if startMon <= now.Month() && now.Month() < startMon+3 {
				// Currently within this year's season: "last" steps back a year.
				year--
			}
		}
	case "this":
		if startMon > now.Month() {
			year--
		}
	case "next", "coming":
		if startMon < now.Month() {
			year++
		}
	default:
		return Window{}, false
	}

	start := time.Date(year, startMon, 1, 0, 0, 0, 0, now.Location())
	return Window{Start: start, End: start.AddDate(0, 3, 0)}, true
}

var yearRe = regexp.MustCompile(`^(\d{4})$`)

// parseYear handles a bare 4-digit year, returning the whole calendar year.
func parseYear(s string, now time.Time) (Window, bool) {
	m := yearRe.FindStringSubmatch(s)
	if m == nil {
		return Window{}, false
	}
	year, _ := strconv.Atoi(m[1])
	// Sanity bound: treat absurd years as unrecognized so a random 4-digit
	// token (e.g. part of a different phrase) doesn't masquerade as a year.
	if year < 1000 || year > 9999 {
		return Window{}, false
	}
	start := time.Date(year, 1, 1, 0, 0, 0, 0, now.Location())
	return Window{Start: start, End: start.AddDate(1, 0, 0)}, true
}
