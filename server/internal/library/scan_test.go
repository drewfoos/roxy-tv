package library

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/store"
)

type scanCall struct {
	lib   string
	force bool
}

// twoLibraries returns a Library over a store with two libraries, its per-library scan
// replaced by a recorder (onScan, when set, runs inside each call), and the libraries
// in the order a full scan visits them.
func twoLibraries(t *testing.T, onScan func()) (*Library, *[]scanCall, *sync.Mutex, []string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	for _, name := range []string{"Anime", "Movies"} {
		if err := st.CreateLibrary(ctx, &model.Library{Name: name, Kind: model.KindSeries, Root: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
	}
	libs, err := st.ListLibraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, l := range libs {
		order = append(order, l.Name)
	}
	l := New(st, nil, nil)
	var mu sync.Mutex
	calls := &[]scanCall{}
	l.scanOne = func(_ context.Context, lib model.Library, force bool) error {
		mu.Lock()
		*calls = append(*calls, scanCall{lib.Name, force})
		mu.Unlock()
		if onScan != nil {
			onScan()
		}
		return nil
	}
	return l, calls, &mu, order
}

func pass(order []string, force bool) []scanCall {
	var out []scanCall
	for _, n := range order {
		out = append(out, scanCall{n, force})
	}
	return out
}

func sameCalls(t *testing.T, got, want []scanCall) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("scans:\n got  %v\n want %v", got, want)
	}
}

// A forced full scan re-probes every library. It used to be a shared flag that the
// first library's scan reset, so the others were scanned normally.
func TestForcedScanCoversEveryLibrary(t *testing.T) {
	l, calls, _, order := twoLibraries(t, nil)
	if err := l.ScanAll(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	sameCalls(t, *calls, pass(order, true))
}

// A forced scan asked for while a normal one runs is not lost and does not leak into the
// running pass: that pass stays normal, then one forced pass covers every library.
// Two forced requests merge into one pass; a normal request during the scan adds none.
func TestForcedScanRequestedDuringNormalScan(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	l, calls, mu, order := twoLibraries(t, func() {
		once.Do(func() { close(started); <-release }) // hold the first library's scan open
	})
	ctx := context.Background()
	done := make(chan error, 1)
	go func() { done <- l.ScanAll(ctx, false) }()
	<-started

	for _, force := range []bool{false, true, true} {
		if err := l.ScanAll(ctx, force); err != nil { // returns at once: a scan is running
			t.Fatal(err)
		}
	}
	if running, _ := l.ScanStatus(); !running {
		t.Fatal("scan not reported as running")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]scanCall(nil), *calls...)
	mu.Unlock()
	sameCalls(t, got, append(pass(order, false), pass(order, true)...))
	if running, _ := l.ScanStatus(); running {
		t.Fatal("still reported as running after the last pass")
	}
}

// Many scan requests at once, forced and not: run with -race. Every call returns, and no
// forced request is left queued once everything has settled.
func TestScanRequestsConcurrently(t *testing.T) {
	l, _, _, _ := twoLibraries(t, nil)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(force bool) {
			defer wg.Done()
			if err := l.ScanAll(ctx, force); err != nil {
				t.Error(err)
			}
		}(i%3 == 0)
	}
	wg.Wait()
	if running, _ := l.ScanStatus(); running {
		t.Fatal("still reported as running")
	}
	select {
	case <-l.forcedNext:
		t.Fatal("a forced pass was left queued with no scan to run it")
	default:
	}
}
