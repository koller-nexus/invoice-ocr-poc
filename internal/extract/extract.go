// Package extract turns OCR text into hypothesized line items and totals.
package extract

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// UnlabeledItem is the placeholder when a price line has no product name.
const UnlabeledItem = "unlabeled item"

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
	FoundTotal         bool
}

const lineDelta = 0.05

var (
	brMoney   = regexp.MustCompile(`(?i)(?:r\$\s*)?(\d{1,3}(?:\.\d{3})*,\d{2}|\d+,\d{2}|\d+\.\d{2})`)
	qtyPrefix = regexp.MustCompile(`(?i)^(?:qtd|qty|quant(?:idade)?)\s*[:x×*]?\s*(\d+(?:[.,]\d+)?)`)
	qtyInline = regexp.MustCompile(`(?i)\b(\d+(?:[.,]\d+)?)\s*(?:[x×*]|un|unid|pcs?)\b`)
	qtyTimes  = regexp.MustCompile(`(?i)^\s*(\d+(?:[.,]\d+)?)\s*[x×*]\s*`)
	totalLine = regexp.MustCompile(`(?i)\b(total|valor\s*total|total\s*geral|amount\s*due)\b`)
	metaLine  = regexp.MustCompile(`(?i)^(cnpj|cpf|cnpj\/cpf|ie|im|inscri[cç][aã]o)\b`)
	payLine   = regexp.MustCompile(`(?i)(forma\s+de\s+pagamento|pagamento|cart[aã]o|dinheiro|\bpix\b)`)
	barcode   = regexp.MustCompile(`^\d{8,}\s*`)
	hasLetter = regexp.MustCompile(`\p{L}`)
	junkDesc  = regexp.MustCompile(`(?i)^[\s×x*=.-]+$`)
)

// Parse hypothesizes items and totals from OCR text. Weak OCR is not treated as fact.
func Parse(ocrText string) Result {
	lines := strings.Split(ocrText, "\n")
	items := make([]Item, 0)
	estimated := 0.0
	foundTotal := false
	pendingDesc := ""
	pendingPay := false

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		if metaLine.MatchString(line) {
			pendingDesc = ""
			pendingPay = false

			continue
		}

		if payLine.MatchString(line) && len(parseAmounts(line)) == 0 {
			pendingDesc = line
			pendingPay = true

			continue
		}

		amounts := parseAmounts(line)
		if len(amounts) == 0 {
			if looksLikeProduct(line) {
				pendingDesc = productName(line)
				pendingPay = false
			}

			continue
		}

		if totalLine.MatchString(line) || pendingPay {
			estimated = amounts[len(amounts)-1]
			foundTotal = true
			pendingDesc = ""
			pendingPay = false

			continue
		}

		items = append(items, itemFromLine(line, amounts, pendingDesc))
		pendingDesc = ""
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
		FoundTotal:         foundTotal,
	}
}

// Incomplete reports that the heuristic extract should not be trusted alone.
func (r Result) Incomplete() bool {
	if len(r.Items) == 0 {
		return true
	}

	if !r.FoundTotal {
		return true
	}

	if math.Abs(r.EstimatedTotal-r.ComputedItemsTotal) > lineDelta {
		return true
	}

	for _, it := range r.Items {
		if it.Description == "" || it.Description == UnlabeledItem {
			return true
		}

		if inconsistentLine(it) {
			return true
		}
	}

	return false
}

func inconsistentLine(it Item) bool {
	if it.Quantity <= 0 || it.UnitAmount <= 0 || it.LineTotal <= 0 {
		return false
	}

	return math.Abs(it.Quantity*it.UnitAmount-it.LineTotal) > lineDelta
}

func itemFromLine(line string, amounts []float64, pendingDesc string) Item {
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
	if desc == "" || junkDesc.MatchString(desc) {
		desc = pendingDesc
	}

	if desc == "" {
		desc = UnlabeledItem
	}

	return Item{
		Description: desc,
		Quantity:    qty,
		UnitAmount:  unit,
		LineTotal:   lineTotal,
	}
}

func looksLikeProduct(line string) bool {
	if metaLine.MatchString(line) || payLine.MatchString(line) {
		return false
	}

	return hasLetter.MatchString(line)
}

func productName(line string) string {
	return strings.TrimSpace(barcode.ReplaceAllString(line, ""))
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

	if m := qtyTimes.FindStringSubmatch(line); len(m) == 2 {
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
	cleaned = qtyTimes.ReplaceAllString(cleaned, " ")
	cleaned = qtyInline.ReplaceAllString(cleaned, " ")
	cleaned = strings.ReplaceAll(cleaned, "=", " ")
	cleaned = strings.Join(strings.Fields(cleaned), " ")

	return strings.TrimSpace(cleaned)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
