package outlook

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientDispatchConcurrency(t *testing.T) {
	client := &Client{reqChan: make(chan func())}
	go func() {
		for request := range client.reqChan {
			request()
		}
	}()
	defer close(client.reqChan)

	const requests = 50
	var running, maximum int32
	var completed atomic.Int32
	var group sync.WaitGroup
	for range requests {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := client.dispatch(func() error {
				active := atomic.AddInt32(&running, 1)
				for {
					current := atomic.LoadInt32(&maximum)
					if active <= current || atomic.CompareAndSwapInt32(&maximum, current, active) {
						break
					}
				}
				completed.Add(1)
				atomic.AddInt32(&running, -1)
				return nil
			}); err != nil {
				t.Errorf("dispatch failed: %v", err)
			}
		}()
	}
	group.Wait()
	if completed.Load() != requests {
		t.Fatalf("completed %d requests, want %d", completed.Load(), requests)
	}
	if maximum != 1 {
		t.Fatalf("dispatcher executed %d requests concurrently, want 1", maximum)
	}
}

func TestDASLQueryBuilder(t *testing.T) {
	start := time.Date(2026, 10, 24, 14, 30, 0, 0, time.UTC)
	tests := []struct {
		name   string
		params SearchParams
		want   string
	}{
		{"empty", SearchParams{}, "@SQL=True"},
		{"query escaped", SearchParams{Query: "O'Brien"}, `@SQL="http://schemas.microsoft.com/mapi/proptag/0x0037001F" LIKE '%O''Brien%'`},
		{"all fields", SearchParams{Query: "report", Sender: "alex@example.com", StartDate: start}, `@SQL="http://schemas.microsoft.com/mapi/proptag/0x0037001F" LIKE '%report%' AND "http://schemas.microsoft.com/mapi/proptag/0x0C1F001F" LIKE '%alex@example.com%' AND "http://schemas.microsoft.com/mapi/proptag/0x0E060040" >= '2026-10-24 14:30:00'`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := buildDASLQuery(test.params); got != test.want {
				t.Errorf("buildDASLQuery() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestUnreadRestriction(t *testing.T) {
	if got, want := buildUnreadRestriction(), "[UnRead] = True"; got != want {
		t.Errorf("buildUnreadRestriction() = %q, want %q", got, want)
	}
}
