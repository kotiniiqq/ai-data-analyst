package agent

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	datePattern   = regexp.MustCompile(`\b\d{4}-\d{2}(-\d{2})?\b`)
	numberPattern = regexp.MustCompile(`\$?\d[\d,]*(\.\d+)?\s?(%|k\b|K\b|M\b|m\b)?`)
)

// Grounded reports whether every number in text comes from nums, the numbers
// the tools returned. A number matches if a tool number rounds to it (0-2
// decimals, or in thousands/millions for "k"/"M"); signs are ignored because
// "fell by 5" quotes a change of -5. Years and small counts (0-12) are allowed.
// It returns the numbers it could not match.
func Grounded(text string, nums []float64) (bool, []string) {
	text = datePattern.ReplaceAllString(text, " ")
	var bad []string
	for _, loc := range numberPattern.FindAllStringIndex(text, -1) {
		if loc[0] > 0 && isWordChar(text[loc[0]-1]) {
			continue // part of a word such as Q2 or gpt4
		}
		raw := strings.TrimSpace(text[loc[0]:loc[1]])
		if !matches(raw, nums) {
			bad = append(bad, raw)
		}
	}
	return len(bad) == 0, bad
}

func isWordChar(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func matches(raw string, nums []float64) bool {
	s := strings.TrimPrefix(raw, "$")
	scale := 1.0
	switch {
	case strings.HasSuffix(s, "k"), strings.HasSuffix(s, "K"):
		scale, s = 1e3, s[:len(s)-1]
	case strings.HasSuffix(s, "M"), strings.HasSuffix(s, "m"):
		scale, s = 1e6, s[:len(s)-1]
	}
	s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	s = strings.ReplaceAll(s, ",", "")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return false
	}
	decimals := 0
	if i := strings.IndexByte(s, '.'); i >= 0 {
		decimals = len(s) - i - 1
	}
	if scale == 1 && decimals == 0 && !strings.HasSuffix(raw, "%") && !strings.HasPrefix(raw, "$") {
		if v <= 12 || (v >= 1990 && v <= 2100) {
			return true // a small count or a year
		}
	}
	for _, n := range nums {
		n = math.Abs(n) / scale
		if roundTo(n, decimals) == v {
			return true
		}
		// "$284,830" may quote 284830.27; "$285k" may quote 284.83k
		if decimals == 0 && math.Round(n) == v {
			return true
		}
	}
	return false
}

func roundTo(x float64, d int) float64 {
	p := math.Pow(10, float64(d))
	return math.Round(x*p) / p
}
