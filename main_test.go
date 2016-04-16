package pester_test

import (
	"testing"

	"github.com/sethgrid/pester"
)

func TestConcurrentRequests(t *testing.T) {
	t.Parallel()

	c := pester.New()
	c.Concurrency = 4
	c.KeepLog = true

	nonExistantURL := "http://localhost:9000/foo"

	_, err := c.Get(nonExistantURL)
	if err == nil {
		t.Fatal("expected to get an error")
	}

	// in the event of an error, let's see what the logs were
	t.Log("\n", c.LogString())

	if got, want := len(c.ErrLog), c.Concurrency*c.MaxRetries; got != want {
		t.Errorf("got %d attempts, want %d", got, want)
	}
}

func TestConcurrentRetry0(t *testing.T) {
	t.Parallel()

	c := pester.New()
	c.Concurrency = 4
	c.MaxRetries = 0
	c.KeepLog = true

	nonExistantURL := "http://localhost:9000/foo"

	_, err := c.Get(nonExistantURL)
	if err == nil {
		t.Fatal("expected to get an error")
	}

	// in the event of an error, let's see what the logs were
	t.Log("\n", c.LogString())

	if got, want := len(c.ErrLog), c.Concurrency; got != want {
		t.Errorf("got %d attempts, want %d", got, want)
	}
}

func TestDefaultBackoff(t *testing.T) {
	t.Parallel()

	c := pester.New()
	c.KeepLog = true

	nonExistantURL := "http://localhost:9000/foo"

	_, err := c.Get(nonExistantURL)
	if err == nil {
		t.Fatal("expected to get an error")
	}

	// in the event of an error, let's see what the logs were
	t.Log("\n", c.LogString())

	if got, want := c.Concurrency, 1; got != want {
		t.Error("got %d, want %d for concurrency", got, want)
	}

	if got, want := len(c.ErrLog), c.MaxRetries; got != want {
		t.Fatalf("got %d errors, want %d", got, want)
	}

	var startTime int64
	for i, e := range c.ErrLog {
		if i == 0 {
			startTime = e.Time.Unix()
			continue
		}
		if got, want := e.Time.Unix(), startTime+int64(i); got != want {
			t.Errorf("got time %d, want %d (%d greater than start time %d)", got, want, i, startTime)
		}
	}

}

func TestLinearJitterBackoff(t *testing.T) {
	t.Parallel()
	c := pester.New()
	c.Backoff = pester.LinearJitterBackoff
	c.KeepLog = true

	nonExistantURL := "http://localhost:9000/foo"

	_, err := c.Get(nonExistantURL)
	if err == nil {
		t.Fatal("expected to get an error")
	}

	// in the event of an error, let's see what the logs were
	t.Log("\n", c.LogString())

	var startTime int64
	var delta int64
	for i, e := range c.ErrLog {
		switch i {
		case 0:
			startTime = e.Time.Unix()
		case 1:
			delta += 1
		case 2:
			delta += 2
		case 3:
			delta += 3
		}

		if got, want := e.Time.Unix(), startTime+delta; withinEpsilon(got, want, 0.0) {
			t.Errorf("got time %d, want %d (within epsilon of start time %d)", got, want, startTime)
		}
	}
}

func TestExponentialBackoff(t *testing.T) {
	t.Parallel()

	c := pester.New()
	c.MaxRetries = 4
	c.Backoff = pester.ExponentialBackoff
	c.KeepLog = true

	nonExistantURL := "http://localhost:9000/foo"

	_, err := c.Get(nonExistantURL)
	if err == nil {
		t.Fatal("expected to get an error")
	}

	// in the event of an error, let's see what the logs were
	t.Log("\n", c.LogString())

	if got, want := len(c.ErrLog), c.MaxRetries; got != want {
		t.Fatalf("got %d errors, want %d", got, want)
	}

	var startTime int64
	var delta int64
	for i, e := range c.ErrLog {
		switch i {
		case 0:
			startTime = e.Time.Unix()
		case 1:
			delta += 2
		case 2:
			delta += 4
		case 3:
			delta += 8
		}
		if got, want := e.Time.Unix(), startTime+delta; got != want {
			t.Errorf("got time %d, want %d (%d greater than start time %d)", got, want, delta, startTime)
		}
	}
}

func withinEpsilon(got, want int64, epslion float64) bool {
	if want <= int64(epslion*float64(got)) || want >= int64(epslion*float64(got)) {
		return false
	}
	return true
}
