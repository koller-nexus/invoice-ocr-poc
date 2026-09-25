// Package extract turns OCR text into hypothesized line items and totals.
package extract

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Item is a hypothesized invoice line.
type Item struct {
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity"`
	UnitAmount  float64 `json:"unit_amount"`
	LineTotal   float64 `json:"line_total"`
}

// Result is a heuristic parse of OCR text.
type Result struct {
	Items              []Item
	EstimatedTotal     float64
	ComputedItemsTotal float64
}

var (
	brMoney   = regexp.MustCompile(`(?i)(?:r\$\s*)?(\d{1,3}(?:\.\d{3})*,\d{2}|\d+,\d{2}|\d+\.\d{2})`)
	qtyPrefix = regexp.MustCompile(`(?i)^(?:qtd|qty|quant(?:idade)?)\s*[:x]?\s*(\d+(?:[.,]\d+)?)`)
	qtyInline = regexp.MustCompile(`(?i)\b(\d+(?:[.,]\d+)?)\s*(?:x|un|unid|pcs?)\b`)
	totalLine = regexp.MustCompile(`(?i)\b(total|valor\s*total|total\s*geral|amount\s*due)\b`)
)

// Parse hypothesizes items and totals from OCR text. Weak OCR is not treated as fact.
func Parse(ocrText string) Result {
	lines := strings.Split(ocrText, "\n")
	items := make([]Item, 0)
	estimated := 0.0
	foundTotal := false

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		amounts := parseAmounts(line)
		if len(amounts) == 0 {
			continue
		}

		if totalLine.MatchString(line) {
			estimated = amounts[len(amounts)-1]
			foundTotal = true

			continue
		}

		qty := parseQty(line)
		unit := amounts[0]
		lineTotal := amounts[len(amounts)-1]

		if qty == 0 {
			qty = 1
		}

		if len(amounts) == 1 {
			lineTotal = unit * qty
		}

		desc := stripNumbers(line)
		if desc == "" {
			desc = "unlabeled item"
		}

		items = append(items, Item{
			Description: desc,
			Quantity:    qty,
			UnitAmount:  unit,
			LineTotal:   lineTotal,
		})
	}

	computed := 0.0
	for _, it := range items {
		if it.LineTotal > 0 {
			computed += it.LineTotal
		} else {
			computed += it.Quantity * it.UnitAmount
		}
	}

	if !foundTotal {
		estimated = computed
	}

	return Result{
		Items:              items,
		EstimatedTotal:     round2(estimated),
		ComputedItemsTotal: round2(computed),
	}
}

func parseAmounts(line string) []float64 {
	matches := brMoney.FindAllStringSubmatch(line, -1)
	out := make([]float64, 0, len(matches))

	for _, m := range matches {
		if v, ok := parseMoney(m[1]); ok {
			out = append(out, v)
		}
	}

	return out
}

func parseMoney(raw string) (float64, bool) {
	s := strings.TrimSpace(raw)
	if strings.Contains(s, ",") {
		s = strings.ReplaceAll(s, ".", "")
		s = strings.ReplaceAll(s, ",", ".")
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}

	return v, true
}

func parseQty(line string) float64 {
	if m := qtyPrefix.FindStringSubmatch(line); len(m) == 2 {
		if v, ok := parseMoney(m[1]); ok {
			return v
		}
	}

	if m := qtyInline.FindStringSubmatch(line); len(m) == 2 {
		if v, ok := parseMoney(m[1]); ok {
			return v
		}
	}

	return 0
}

func stripNumbers(line string) string {
	cleaned := brMoney.ReplaceAllString(line, " ")
	cleaned = qtyPrefix.ReplaceAllString(cleaned, " ")
	cleaned = qtyInline.ReplaceAllString(cleaned, " ")
	cleaned = strings.Join(strings.Fields(cleaned), " ")

	return strings.TrimSpace(cleaned)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
