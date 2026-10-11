package agent

import (
	"slices"
	"testing"
)

var nums = []float64{284830.27, 280000.5, 4829.77, 1.72, 75250.82, -464.92, 12, 0.0032}

func TestGroundedNumbersPass(t *testing.T) {
	for _, text := range []string{
		"MRR was $284,830.27 in September 2026.",
		"MRR rose by 4,829.77 USD (1.72%) over 12 months.",
		"MRR went from $280,000.50 to $284,830.27.",
		"About $284.8k in MRR, roughly $285k.",
		"Team MRR: 75250.82; churn fell by 464.92.",
		"In 2025-10 .. 2026-09, Q2 2026 included, 3 plans.",
		"It would cost about $0.0032.",
		"Nothing to report.",
	} {
		if ok, bad := Grounded(text, nums); !ok {
			t.Errorf("%q rejected, bad: %v", text, bad)
		}
	}
}

func TestInventedNumbersFail(t *testing.T) {
	for text, want := range map[string]string{
		"MRR grew 12.5% this year.":           "12.5%",
		"MRR was $290,000 in September.":      "$290,000",
		"That is about $300k.":                "$300k",
		"Margin is 73.6% and MRR 284,830.27.": "73.6%",
		"There are 45 customers.":             "45",
	} {
		ok, bad := Grounded(text, nums)
		if ok || !slices.Contains(bad, want) {
			t.Errorf("%q: ok=%v bad=%v, want %q flagged", text, ok, bad, want)
		}
	}
}
