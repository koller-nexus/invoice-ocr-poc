package ocr

import "testing"

func TestCleanText(t *testing.T) {
	t.Parallel()

	const mercury = `MERCURY SUPERMERCADOS LTDA
CNPJ: 12.345.678/0001-90
----------------------------------------------------
24/09/2026 12:10
----------------------------------------------------
7891000001234 Arroz Tipo 1 5kg 1 UN 24,90 24,90
------------------------------------------
SUBTOTAL 59,37
TOTAL R$ 58,87
------------------------------------------
OBRIGADO
PELA PREFERÊNCIA`

	wantMercury := `MERCURY SUPERMERCADOS LTDA
CNPJ: 12.345.678/0001-90
----------------------------------------------------
24/09/2026 12:10
----------------------------------------------------
7891000001234 Arroz Tipo 1 5kg 1 UN 24,90 24,90
------------------------------------------
SUBTOTAL 59,37
TOTAL R$ 58,87
------------------------------------------
OBRIGADO
PELA PREFERÊNCIA`

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "mercury keeps mid dividers drops trailing seps",
			in:   mercury + "\n------------------------------------------\n------------------------------------------\n------------------------------------------\n------------------------------------------",
			want: wantMercury,
		},
		{
			name: "collapses blank lines",
			in:   "CAFE\n\n\n\nTOTAL 10,00",
			want: "CAFE\n\nTOTAL 10,00",
		},
		{
			name: "collapses consecutive separators",
			in:   "A\n-----\n-----\n-----\nB",
			want: "A\n-----\nB",
		},
		{
			name: "strips fences then seps",
			in:   "```markdown\nCAFE\n-----\n-----\nTOTAL 10,00\n```",
			want: "CAFE\n-----\nTOTAL 10,00",
		},
		{
			name: "only separators",
			in:   "-----\n-----\n",
			want: "",
		},
		{
			name: "empty",
			in:   "   ",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := CleanText(tc.in)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
