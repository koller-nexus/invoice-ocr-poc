package invoice

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/williamkoller/invoice-ocr-poc/internal/cache"
	"github.com/williamkoller/invoice-ocr-poc/internal/extract"
	"github.com/williamkoller/invoice-ocr-poc/internal/jev"
	"github.com/williamkoller/invoice-ocr-poc/internal/ocr"
	"github.com/williamkoller/invoice-ocr-poc/internal/store"
	"github.com/williamkoller/invoice-ocr-poc/internal/worker"
)

type okJobs struct{}

func (okJobs) Enqueue(context.Context, *store.Invoice) error { return nil }

type fullJobs struct{}

func (fullJobs) Enqueue(context.Context, *store.Invoice) error { return ErrQueueFull }

func TestEnqueue_QueueFullRollsBack(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	st := openTestStore(t, dir)

	svc := NewService(st, fullJobs{}, dir, 1024, nil, nil)
	_, err := svc.Enqueue(t.Context(), EnqueueInput{
		OriginalName: "note.png",
		MIMEType:     "image/png",
		Body:         bytes.NewReader([]byte("\x89PNG\r\n\x1a\n")),
	})
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("got %v", err)
	}

	rows, err := st.List(t.Context(), 20, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 0 {
		t.Fatalf("expected no invoices, got %d", len(rows))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if e.Name() == "t.db" || e.Name() == "t.db-wal" || e.Name() == "t.db-shm" {
			continue
		}

		t.Fatalf("leftover file %s", e.Name())
	}
}

func TestEnqueue_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	st := openTestStore(t, dir)

	svc := NewService(st, okJobs{}, dir, 1024, nil, nil)
	inv, err := svc.Enqueue(t.Context(), EnqueueInput{
		OriginalName: "note.png",
		MIMEType:     "image/png",
		Body:         bytes.NewReader([]byte("\x89PNG\r\n\x1a\n")),
	})
	if err != nil {
		t.Fatal(err)
	}

	if inv.Status != store.StatusQueued {
		t.Fatalf("status %s", inv.Status)
	}
}

const (
	strongOCRText   = "Cafe 2x 5,00 10,00\nPao 1 un 3,50\nTotal R$ 13,50"
	shortOCRText    = "hi"
	noItemsOCRText  = "hello world this is long enough text without any money amounts here"
	mismatchOCRText = "Cafe 2x 5,00 10,00\nPao 1 un 3,50\nTotal R$ 99,00"
	messyOCRText    = "CNPJ: 8/0001-90\n" +
		"1 × 12.34 = 5.67\n" +
		"IE: ISENTO IM: -9\n" +
		"1 × 123.45 = 6.78\n" +
		"7891300001122 Pão Francês 500g\n" +
		"1 × 7.20 = 7.20\n" +
		"FORMA DE PAGAMENTO: Cartão Débito\n" +
		"1 × 58.87 = 58.87\n"
)

type fakeOCR struct {
	available bool
	text      string
	calls     atomic.Int32
}

func (f *fakeOCR) Recognize(ctx context.Context, _ string) (ocr.Result, error) {
	if err := ctx.Err(); err != nil {
		return ocr.Result{}, err
	}

	f.calls.Add(1)

	return ocr.Result{Text: f.text}, nil
}

func (f *fakeOCR) Available() bool {
	return f.available
}

type fakeAssist struct {
	enabled bool
	calls   int
	out     extract.Result
}

func (f *fakeAssist) Enabled() bool { return f.enabled }

func (f *fakeAssist) Model() string { return "stub-assist" }

func (f *fakeAssist) Assist(
	_ context.Context,
	_ string,
	_ extract.Result,
) (extract.Result, string, ocr.Usage, error) {
	f.calls++

	return f.out, "ok", ocr.Usage{}, nil
}

func TestNeedsEscalateAndAssist(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		text         string
		wantEscalate bool
		wantAssist   bool
	}{
		{name: "strong extract", text: strongOCRText, wantEscalate: false, wantAssist: false},
		{name: "short text", text: shortOCRText, wantEscalate: true, wantAssist: true},
		{name: "no items", text: noItemsOCRText, wantEscalate: true, wantAssist: true},
		{name: "divergent totals", text: mismatchOCRText, wantEscalate: true, wantAssist: true},
		{name: "messy tesseract receipt", text: messyOCRText, wantEscalate: true, wantAssist: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parsed := extract.Parse(tc.text)
			if got := needsEscalate(tc.text, parsed); got != tc.wantEscalate {
				t.Fatalf("needsEscalate=%v want %v items=%d", got, tc.wantEscalate, len(parsed.Items))
			}

			if got := needsAssist(parsed); got != tc.wantAssist {
				t.Fatalf("needsAssist=%v want %v", got, tc.wantAssist)
			}
		})
	}
}

type stageOCRCase struct {
	name       string
	tess       *fakeOCR
	ollama     *fakeOCR
	wantTess   int
	wantOllama int
	wantItems  int
}

func cascadeCases() []stageOCRCase {
	return []stageOCRCase{
		{
			name:       "tesseract strong skips ollama",
			tess:       &fakeOCR{available: true, text: strongOCRText},
			ollama:     &fakeOCR{available: true, text: noItemsOCRText},
			wantTess:   1,
			wantOllama: 0,
			wantItems:  2,
		},
		{
			name:       "no items tesseract escalates to ollama",
			tess:       &fakeOCR{available: true, text: noItemsOCRText},
			ollama:     &fakeOCR{available: true, text: strongOCRText},
			wantTess:   1,
			wantOllama: 1,
			wantItems:  2,
		},
		{
			name:       "short tesseract escalates to ollama",
			tess:       &fakeOCR{available: true, text: shortOCRText},
			ollama:     &fakeOCR{available: true, text: strongOCRText},
			wantTess:   1,
			wantOllama: 1,
			wantItems:  2,
		},
		{
			name:       "messy tesseract with items skips ollama",
			tess:       &fakeOCR{available: true, text: messyOCRText},
			ollama:     &fakeOCR{available: true, text: strongOCRText},
			wantTess:   1,
			wantOllama: 0,
			wantItems:  3,
		},
		{
			name:       "tesseract unavailable uses ollama only",
			tess:       &fakeOCR{available: false, text: strongOCRText},
			ollama:     &fakeOCR{available: true, text: strongOCRText},
			wantTess:   0,
			wantOllama: 1,
			wantItems:  2,
		},
		{
			name:       "nil tesseract uses ollama only",
			ollama:     &fakeOCR{available: true, text: strongOCRText},
			wantTess:   0,
			wantOllama: 1,
			wantItems:  2,
		},
	}
}

func TestStageOCR_Cascade(t *testing.T) {
	t.Parallel()

	for _, tc := range cascadeCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStageOCRCase(t, tc)
		})
	}
}

func runStageOCRCase(t *testing.T, tc stageOCRCase) {
	t.Helper()

	dir := t.TempDir()
	st := openTestStore(t, dir)

	svc := NewService(st, okJobs{}, dir, 1024, nil, nil)
	inv := &store.Invoice{ID: "inv-1", Status: store.StatusProcessing}

	var tess ocr.Engine
	if tc.tess != nil {
		tess = tc.tess
	}

	j := &worker.Job{
		ID:      inv.ID,
		Inv:     inv,
		Started: time.Now(),
		Ctx:     t.Context(),
		ImgPath: "x.png",
	}

	err := svc.StageOCR(t.Context(), j, OCRInput{
		Tess:   tess,
		Ollama: tc.ollama,
	})
	if err != nil {
		t.Fatal(err)
	}

	if tc.tess != nil && int(tc.tess.calls.Load()) != tc.wantTess {
		t.Fatalf("tess calls=%d want %d", tc.tess.calls.Load(), tc.wantTess)
	}

	if tc.ollama != nil && int(tc.ollama.calls.Load()) != tc.wantOllama {
		t.Fatalf("ollama calls=%d want %d", tc.ollama.calls.Load(), tc.wantOllama)
	}

	if len(j.Parsed.Items) != tc.wantItems {
		t.Fatalf("items=%d want %d", len(j.Parsed.Items), tc.wantItems)
	}
}

func openTestStore(t *testing.T, dir string) *store.Store {
	t.Helper()

	st, err := store.Open(t.Context(), filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = st.Close() })

	return st
}

type fakeJudge struct {
	calls atomic.Int32
}

func (f *fakeJudge) Evaluate(context.Context, jev.State) (jev.Judgment, jev.Usage, error) {
	f.calls.Add(1)

	return jev.Judgment{DocumentType: "receipt", EffectiveRouting: "other"}, jev.Usage{}, nil
}

func TestStageJev_AssistParallel(t *testing.T) {
	t.Parallel()

	strongParsed := extract.Parse(strongOCRText)
	weakParsed := extract.Parse(mismatchOCRText)
	sameAsWeak := weakParsed
	changed := extract.Result{
		Items: []extract.Item{{
			Description: "assisted",
			Quantity:    1,
			UnitAmount:  1,
			LineTotal:   1,
		}},
		EstimatedTotal:     1,
		ComputedItemsTotal: 1,
		FoundTotal:         true,
	}

	cases := []struct {
		name       string
		parsed     extract.Result
		assistOut  extract.Result
		wantAssist int
		wantJudge  int
		wantItems  int
	}{
		{
			name:       "strong extract skips assist and judges once",
			parsed:     strongParsed,
			assistOut:  changed,
			wantAssist: 0,
			wantJudge:  1,
			wantItems:  2,
		},
		{
			name:       "weak extract assist unchanged judges once",
			parsed:     weakParsed,
			assistOut:  sameAsWeak,
			wantAssist: 1,
			wantJudge:  1,
			wantItems:  2,
		},
		{
			name:       "weak extract assist changed judges twice",
			parsed:     weakParsed,
			assistOut:  changed,
			wantAssist: 1,
			wantJudge:  2,
			wantItems:  1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			st := openTestStore(t, dir)

			svc := NewService(st, okJobs{}, dir, 1024, nil, nil)
			inv := &store.Invoice{ID: "inv-1", Status: store.StatusProcessing}
			assist := &fakeAssist{enabled: true, out: tc.assistOut}
			judge := &fakeJudge{}
			j := &worker.Job{
				ID:      inv.ID,
				Inv:     inv,
				Started: time.Now(),
				Ctx:     t.Context(),
				OCRRes:  ocr.Result{Text: "ocr"},
				Parsed:  tc.parsed,
			}

			err := svc.StageJev(t.Context(), j, JevInput{Judge: judge, Assist: assist})
			if err != nil {
				t.Fatal(err)
			}

			if assist.calls != tc.wantAssist {
				t.Fatalf("assist calls=%d want %d", assist.calls, tc.wantAssist)
			}

			if int(judge.calls.Load()) != tc.wantJudge {
				t.Fatalf("judge calls=%d want %d", judge.calls.Load(), tc.wantJudge)
			}

			if len(j.Parsed.Items) != tc.wantItems {
				t.Fatalf("items=%d want %d", len(j.Parsed.Items), tc.wantItems)
			}

			if inv.Status != store.StatusDone {
				t.Fatalf("status %s", inv.Status)
			}
		})
	}
}

func TestStageJev_OpenRouterFallback(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		text           string
		fallback       *fakeOCR
		wantFallback   int
		wantAssist     int
		wantItems      int
		wantConfidence float64
	}{
		{
			name:           "strong extract skips openrouter and assist",
			text:           strongOCRText,
			fallback:       &fakeOCR{available: true, text: noItemsOCRText},
			wantFallback:   0,
			wantAssist:     0,
			wantItems:      2,
			wantConfidence: 1,
		},
		{
			name:           "weak extract uses openrouter then skips assist",
			text:           shortOCRText,
			fallback:       &fakeOCR{available: true, text: strongOCRText},
			wantFallback:   1,
			wantAssist:     0,
			wantItems:      2,
			wantConfidence: 1,
		},
		{
			name:           "weak extract and weak openrouter still assists",
			text:           shortOCRText,
			fallback:       &fakeOCR{available: true, text: noItemsOCRText},
			wantFallback:   1,
			wantAssist:     1,
			wantItems:      0,
			wantConfidence: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			st := openTestStore(t, dir)
			svc := NewService(st, okJobs{}, dir, 1024, nil, nil)
			parsed := extract.Parse(tc.text)
			conf := extractConfidence(tc.text, parsed)
			inv := &store.Invoice{ID: "inv-1", Status: store.StatusProcessing, OCRConfidence: conf}
			assist := &fakeAssist{enabled: true, out: parsed}
			judge := &fakeJudge{}
			j := &worker.Job{
				ID:      inv.ID,
				Inv:     inv,
				Started: time.Now(),
				Ctx:     t.Context(),
				ImgPath: "x.png",
				OCRRes:  ocr.Result{Text: tc.text, Confidence: conf},
				Parsed:  parsed,
			}

			err := svc.StageJev(t.Context(), j, JevInput{
				Judge:      judge,
				Assist:     assist,
				OpenRouter: tc.fallback,
			})
			if err != nil {
				t.Fatal(err)
			}

			if int(tc.fallback.calls.Load()) != tc.wantFallback {
				t.Fatalf("openrouter calls=%d want %d", tc.fallback.calls.Load(), tc.wantFallback)
			}

			if assist.calls != tc.wantAssist {
				t.Fatalf("assist calls=%d want %d", assist.calls, tc.wantAssist)
			}

			if len(j.Parsed.Items) != tc.wantItems {
				t.Fatalf("items=%d want %d", len(j.Parsed.Items), tc.wantItems)
			}

			if inv.OCRConfidence != tc.wantConfidence {
				t.Fatalf("confidence %v want %v", inv.OCRConfidence, tc.wantConfidence)
			}
		})
	}
}

func TestFail_TimeoutMessage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	st := openTestStore(t, dir)
	svc := NewService(st, okJobs{}, dir, 1024, nil, nil)
	svc.Configure(0.75, time.Millisecond, time.Second, time.Second)

	blocked := &blockOCR{}
	inv := &store.Invoice{ID: "inv-1", Status: store.StatusProcessing}
	j := &worker.Job{
		ID:      inv.ID,
		Inv:     inv,
		Started: time.Now(),
		Ctx:     t.Context(),
		ImgPath: "x.png",
	}

	err := svc.StageOCR(t.Context(), j, OCRInput{Tess: blocked})
	if err == nil {
		t.Fatal("expected timeout")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}

	if inv.Status != store.StatusFailed {
		t.Fatalf("status %s", inv.Status)
	}

	if !strings.Contains(inv.ErrorMessage, "timeout") {
		t.Fatalf("error_message %q", inv.ErrorMessage)
	}
}

type blockOCR struct{}

func (blockOCR) Recognize(ctx context.Context, _ string) (ocr.Result, error) {
	<-ctx.Done()

	return ocr.Result{}, ctx.Err()
}

func (blockOCR) Available() bool { return true }

func TestStageJev_CacheHitSkipsJev(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	st := openTestStore(t, dir)
	svc := NewService(st, okJobs{}, dir, 1024, nil, nil)
	svc.SetCache(cache.New(time.Hour, true))

	// First pass: cache miss, judge is called
	judge1 := &fakeJudge{}
	inv1 := &store.Invoice{ID: "inv-1", Status: store.StatusProcessing}
	j1 := &worker.Job{
		ID:      inv1.ID,
		Inv:     inv1,
		Started: time.Now(),
		Ctx:     t.Context(),
		OCRRes:  ocr.Result{Text: strongOCRText, Confidence: 1},
		Parsed:  extract.Parse(strongOCRText),
	}

	err := svc.StageJev(t.Context(), j1, JevInput{Judge: judge1, Assist: &fakeAssist{}})
	if err != nil {
		t.Fatal(err)
	}

	if judge1.calls.Load() != 1 {
		t.Fatalf("expected judge called once, got %d", judge1.calls.Load())
	}

	// Second pass: same OCR text, cache hit, judge NOT called
	judge2 := &fakeJudge{}
	inv2 := &store.Invoice{ID: "inv-2", Status: store.StatusProcessing}
	j2 := &worker.Job{
		ID:       inv2.ID,
		Inv:      inv2,
		Started:  time.Now(),
		Ctx:      t.Context(),
		OCRRes:   ocr.Result{Text: strongOCRText, Confidence: 1},
		Parsed:   extract.Parse(strongOCRText),
		CacheHit: true,
		CacheEntry: &cache.Entry{
			OCRText:       strongOCRText,
			OCRConfidence: 1,
			Parsed:        extract.Parse(strongOCRText),
			Judgment:      jev.Judgment{DocumentType: "receipt"},
		},
	}

	err = svc.StageJev(t.Context(), j2, JevInput{Judge: judge2, Assist: &fakeAssist{}})
	if err != nil {
		t.Fatal(err)
	}

	if judge2.calls.Load() != 0 {
		t.Fatalf("expected judge NOT called on cache hit, got %d", judge2.calls.Load())
	}

	if inv2.Status != store.StatusDone {
		t.Fatalf("status %s", inv2.Status)
	}
}

func TestStageOCR_PopulatesCacheHit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	st := openTestStore(t, dir)
	c := cache.New(time.Hour, true)
	svc := NewService(st, okJobs{}, dir, 1024, nil, nil)
	svc.SetCache(c)

	// Pre-populate cache
	c.Set(strongOCRText, &cache.Entry{
		OCRText:       strongOCRText,
		OCRConfidence: 1,
		Parsed:        extract.Parse(strongOCRText),
		Judgment:      jev.Judgment{DocumentType: "receipt"},
	})

	inv := &store.Invoice{ID: "inv-1", Status: store.StatusProcessing}
	j := &worker.Job{
		ID:      inv.ID,
		Inv:     inv,
		Started: time.Now(),
		Ctx:     t.Context(),
		ImgPath: "x.png",
	}

	tess := &fakeOCR{available: true, text: strongOCRText}
	err := svc.StageOCR(t.Context(), j, OCRInput{Tess: tess})
	if err != nil {
		t.Fatal(err)
	}

	if !j.CacheHit {
		t.Fatal("expected CacheHit=true")
	}

	if j.CacheEntry == nil {
		t.Fatal("expected CacheEntry to be set")
	}
}
