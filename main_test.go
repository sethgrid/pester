package pester_test

import (
	"testing"

	"github.com/sethgrid/pester"
)

func TestDefaultBackoff(t *testing.T) {
	c := pester.New()
	c.Concurrency = 4
	c.KeepLog = true

	nonExistantURL := "http://localhost:9000/foo"

	_, err := c.Get(nonExistantURL)
	if err == nil {
		t.Fatal("expected to get an error")
	}

	for _, entry := range c.ErrLog {
		t.Logf(entry)
	}
	t.Fail()
}

func TestExponentialBackoff(t *testing.T) {
	c := pester.New()
	c.MaxRetries = 4
	c.Backoff = pester.ExponentialBackoff
	c.KeepLog = true

	nonExistantURL := "http://localhost:9000/foo"

	_, err := c.Get(nonExistantURL)
	if err == nil {
		t.Fatal("expected to get an error")
	}

	for _, entry := range c.ErrLog {
		t.Logf(entry)
	}
	t.Fail()
}
