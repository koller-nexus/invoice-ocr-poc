package jev

import (
	"testing"

	"github.com/williamkoller/invoice-ocr-poc/internal/store"
)

func TestExplain_Determinants(t *testing.T) {
	t.Parallel()

	ok := store.Invoice{
		ID:                 "ok",
		DocumentType:       "receipt",
		DocumentTypeConf:   0.9,
		SuggestedRouting:   "fiscal",
		RoutingConfidence:  0.85,
		EffectiveRouting:   "fiscal",
		AmountsSupported:   0.8,
		ItemsQtySupported:  0.8,
		TotalConsistent:    0.8,
		RiskScore:          0.2,
		RiskLabel:          "low",
		RiskConfidence:     0.8,
		EstimatedTotal:     10,
		ComputedItemsTotal: 10,
		NeedsReview:        false,
		ItemsConfirmed:     true,
	}

	cases := []struct {
		name      string
		inv       store.Invoice
		wantIDs   []string
		wantEmpty bool
	}{
		{
			name:      "all clear",
			inv:       ok,
			wantEmpty: true,
		},
		{
			name: "low amounts noul",
			inv: func() store.Invoice {
				inv := ok
				inv.ID = "amounts"
				inv.AmountsSupported = 0.21
				inv.NeedsReview = true
				inv.ItemsConfirmed = false
				return inv
			}(),
			wantIDs: []string{"amounts_supported"},
		},
		{
			name: "numeric mismatch",
			inv: func() store.Invoice {
				inv := ok
				inv.ID = "mismatch"
				inv.EstimatedTotal = 20
				inv.ComputedItemsTotal = 10
				inv.NeedsReview = true
				inv.ItemsConfirmed = false
				return inv
			}(),
			wantIDs: []string{"item_total_mismatch"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Explain(tc.inv)
			if got.InvoiceID != tc.inv.ID {
				t.Fatalf("id %s", got.InvoiceID)
			}

			if len(got.Questions) != 6 {
				t.Fatalf("questions %d", len(got.Questions))
			}

			if tc.wantEmpty {
				if len(got.Determinants) != 0 {
					t.Fatalf("determinants %+v", got.Determinants)
				}

				return
			}

			gotIDs := make(map[string]bool, len(got.Determinants))
			for _, d := range got.Determinants {
				gotIDs[d.ID] = true
			}

			for _, id := range tc.wantIDs {
				if !gotIDs[id] {
					t.Fatalf("missing determinant %s in %+v", id, got.Determinants)
				}
			}
		})
	}
}
