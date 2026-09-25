package extract

import "testing"

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
