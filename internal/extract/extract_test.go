package extract

import (
	"strings"
	"testing"
)

func TestParse_ItemsAndTotal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		text          string
		wantItems     int
		wantEstimated float64
		wantComputed  float64
	}{
		{
			name:          "line items and total",
			text:          "Cafe 2x 5,00 10,00\nPao 1 un 3,50\nTotal R$ 13,50",
			wantItems:     2,
			wantEstimated: 13.50,
			wantComputed:  13.50,
		},
		{
			name:          "no amounts",
			text:          "hello world",
			wantItems:     0,
			wantEstimated: 0,
			wantComputed:  0,
		},
		{
			name: "qty times price uses previous product line",
			text: "CNPJ: 8/0001-90\n" +
				"1 × 12.34 = 5.67\n" +
				"IE: ISENTO IM: -9\n" +
				"1 × 123.45 = 6.78\n" +
				"7891300001122 Pão Francês 500g\n" +
				"1 × 7.20 = 7.20\n" +
				"FORMA DE PAGAMENTO: Cartão Débito\n" +
				"1 × 58.87 = 58.87\n",
			wantItems:     3,
			wantEstimated: 58.87,
			wantComputed:  19.65,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Parse(tc.text)
			if len(got.Items) != tc.wantItems {
				t.Fatalf("items=%d want %d (%+v)", len(got.Items), tc.wantItems, got.Items)
			}

			if got.EstimatedTotal != tc.wantEstimated {
				t.Fatalf("estimated=%v want %v", got.EstimatedTotal, tc.wantEstimated)
			}

			if got.ComputedItemsTotal != tc.wantComputed {
				t.Fatalf("computed=%v want %v", got.ComputedItemsTotal, tc.wantComputed)
			}
		})
	}
}

func TestParse_MessyTesseractReceipt(t *testing.T) {
	t.Parallel()

	text := "CNPJ: 8/0001-90\n" +
		"1 × 12.34 = 5.67\n" +
		"IE: ISENTO IM: -9\n" +
		"1 × 123.45 = 6.78\n" +
		"7891300001122 Pão Francês 500g\n" +
		"1 × 7.20 = 7.20\n" +
		"FORMA DE PAGAMENTO: Cartão Débito\n" +
		"1 × 58.87 = 58.87\n"

	got := Parse(text)
	if !got.FoundTotal {
		t.Fatal("expected found total from payment line")
	}

	if !got.Incomplete() {
		t.Fatal("messy tesseract extract must be incomplete")
	}

	foundBread := false
	for _, it := range got.Items {
		if strings.Contains(it.Description, "Pão Francês") && it.LineTotal == 7.2 {
			foundBread = true
		}
	}

	if !foundBread {
		t.Fatalf("missing Pão Francês 7.20 in %+v", got.Items)
	}
}
