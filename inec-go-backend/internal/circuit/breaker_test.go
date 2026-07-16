package circuit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestBreakerClosedOnInit(t *testing.T) {
	b := New(Config{Name: "test", MaxFailures: 3, CooldownDuration: 100 * time.Millisecond})
	if b.State() != Closed {
		t.Fatalf("expected Closed, got %v", b.State())
	}
}

func TestBreakerOpensAfterMaxFailures(t *testing.T) {
	b := New(Config{Name: "test", MaxFailures: 2, CooldownDuration: 100 * time.Millisecond})
	fail := errors.New("fail")

	for i := 0; i < 2; i++ {
		b.Do(context.Background(), func(ctx context.Context) error { return fail })
	}
	if b.State() != Open {
		t.Fatalf("expected Open after 2 failures, got %v", b.State())
	}
}

func TestBreakerRejectsWhenOpen(t *testing.T) {
	b := New(Config{Name: "test", MaxFailures: 1, CooldownDuration: 1 * time.Second})
	b.Do(context.Background(), func(ctx context.Context) error { return errors.New("fail") })

	err := b.Do(context.Background(), func(ctx context.Context) error { return nil })
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("expected ErrOpen (wrapped), got %v", err)
	}
}

func TestBreakerTransitionsToHalfOpen(t *testing.T) {
	b := New(Config{Name: "test", MaxFailures: 1, CooldownDuration: 50 * time.Millisecond})
	b.Do(context.Background(), func(ctx context.Context) error { return errors.New("fail") })

	time.Sleep(60 * time.Millisecond)

	err := b.Do(context.Background(), func(ctx context.Context) error { return nil })
	if err != nil {
		t.Fatalf("expected nil in half-open, got %v", err)
	}
	if b.State() != Closed {
		t.Fatalf("expected Closed after success in half-open, got %v", b.State())
	}
}

func TestBreakerClosesOnSuccess(t *testing.T) {
	b := New(Config{Name: "test", MaxFailures: 3, CooldownDuration: 100 * time.Millisecond})

	b.Do(context.Background(), func(ctx context.Context) error { return errors.New("fail") })
	b.Do(context.Background(), func(ctx context.Context) error { return errors.New("fail") })
	b.Do(context.Background(), func(ctx context.Context) error { return nil })

	if b.State() != Closed {
		t.Fatalf("expected Closed, got %v", b.State())
	}

	stats := b.Stats()
	if stats.State != "closed" {
		t.Fatalf("expected closed state, got %s", stats.State)
	}
}

func TestRegistryGetAndAll(t *testing.T) {
	r := NewRegistry()
	r.Register(Config{Name: "redis", MaxFailures: 3, CooldownDuration: time.Second})
	r.Register(Config{Name: "kafka", MaxFailures: 5, CooldownDuration: time.Second})

	b := r.Get("redis")
	if b == nil {
		t.Fatal("expected breaker for 'redis'")
	}
	// Get auto-creates if not found — verify it creates with defaults
	unknown := r.Get("auto-created")
	if unknown == nil {
		t.Fatal("expected auto-created breaker")
	}

	all := r.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 breakers (2 registered + 1 auto), got %d", len(all))
	}
}

func TestStateChangeCallback(t *testing.T) {
	var mu sync.Mutex
	var gotFrom, gotTo State
	called := false

	b := New(Config{
		Name:             "test",
		MaxFailures:      1,
		CooldownDuration: 50 * time.Millisecond,
		OnStateChange: func(name string, from, to State) {
			mu.Lock()
			gotFrom = from
			gotTo = to
			called = true
			mu.Unlock()
		},
	})

	b.Do(context.Background(), func(ctx context.Context) error { return errors.New("fail") })

	// OnStateChange is called in a goroutine, need to wait
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Fatal("expected callback to be called")
	}
	if gotFrom != Closed || gotTo != Open {
		t.Fatalf("expected Closed→Open, got %v→%v", gotFrom, gotTo)
	}
}
