package dedup

import (
	"context"
	"errors"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

type stubFilter struct {
	seen       bool
	openErr    error
	closeErr   error
	opened     bool
	closed     bool
	seenCalled int
}

func (f *stubFilter) Open(ctx context.Context) error {
	if f.openErr != nil {
		return f.openErr
	}
	f.opened = true
	return nil
}

func (f *stubFilter) Close(reason string) error {
	f.closed = true
	return f.closeErr
}

func (f *stubFilter) RequestSeen(request *shttp.Request) bool {
	f.seenCalled++
	return f.seen
}

func TestCompositeDupeFilter_RequestSeenOrSemantics(t *testing.T) {
	t.Parallel()

	first := &stubFilter{seen: false}
	second := &stubFilter{seen: true}
	third := &stubFilter{seen: false}
	filter := NewCompositeDupeFilter(first, nil, second, third)

	if !filter.RequestSeen(shttp.MustNewRequest("https://example.com")) {
		t.Fatal("expected composite to report duplicate when one child matches")
	}
	if first.seenCalled != 1 || second.seenCalled != 1 || third.seenCalled != 0 {
		t.Fatalf("unexpected call counts: first=%d second=%d third=%d", first.seenCalled, second.seenCalled, third.seenCalled)
	}
	if len(filter.Filters()) != 3 {
		t.Fatalf("Filters() len = %d, want 3", len(filter.Filters()))
	}
}

func TestCompositeDupeFilter_OpenRollback(t *testing.T) {
	t.Parallel()

	first := &stubFilter{}
	second := &stubFilter{openErr: errors.New("boom")}
	filter := NewCompositeDupeFilter(first, second)

	if err := filter.Open(context.Background()); err == nil {
		t.Fatal("Open() expected error")
	}
	if !first.opened || !first.closed {
		t.Fatalf("first filter should be opened then rolled back: opened=%v closed=%v", first.opened, first.closed)
	}
	if second.opened {
		t.Fatal("second filter should not be marked opened")
	}
}

func TestCompositeDupeFilter_CloseJoinsErrors(t *testing.T) {
	t.Parallel()

	errA := errors.New("a")
	errB := errors.New("b")
	filter := NewCompositeDupeFilter(&stubFilter{closeErr: errA}, &stubFilter{closeErr: errB})

	err := filter.Close("done")
	if !errors.Is(err, errA) || !errors.Is(err, errB) {
		t.Fatalf("Close() error = %v, want joined errors", err)
	}
}

func TestNewDupeFilterDefault(t *testing.T) {
	t.Parallel()

	filter, err := NewDupeFilter(nil)
	if err != nil {
		t.Fatalf("NewDupeFilter(nil) error = %v", err)
	}
	if len(filter.Filters()) != 2 {
		t.Fatalf("default filter count = %d, want 2", len(filter.Filters()))
	}
}

func TestNewDupeFilterRequiresStrategy(t *testing.T) {
	t.Parallel()

	_, err := NewDupeFilter(&Options{})
	if err == nil {
		t.Fatal("NewDupeFilter() expected error when no strategy is enabled")
	}
}
