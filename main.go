package pester

import (
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"
)

type BackoffStrategy func(retry int) time.Duration

type Client struct {
	hc http.Client

	Concurrency int
	MaxRetries  int
	Backoff     BackoffStrategy
	KeepLog     bool

	sync.Mutex
	ErrLog []string
}

func New() *Client {
	return &Client{Concurrency: 1, MaxRetries: 3, Backoff: DefaultBackoff, ErrLog: []string{}}
}

func DefaultBackoff(_ int) time.Duration {
	return 1 * time.Second
}

func ExponentialBackoff(i int) time.Duration {
	return time.Duration(math.Pow(2, float64(i))) * time.Second
}

func (c *Client) Get(url string) (*http.Response, error) {
	type result struct {
		resp *http.Response
		err  error
	}
	resultCh := make(chan result)

	for req := 0; req < c.Concurrency; req++ {
		go func(n int) {
			resp := &http.Response{}
			var err error

			for i := 0; i < c.MaxRetries; i++ {
				resp, err = c.hc.Get(url)
				if err == nil && resp.StatusCode < 400 {
					resultCh <- result{resp: resp, err: err}
				}
				c.log(fmt.Sprintf("GET %s [req %d::ret %d] :: %s", url, n, i, err.Error()))
				<-time.Tick(c.Backoff(i))
			}
			resultCh <- result{resp: resp, err: err}
		}(req)
	}

	for {
		select {
		case res := <-resultCh:
			return res.resp, res.err
		}
	}

	return nil, nil
}

func (c *Client) log(msg string) {
	if c.KeepLog {
		c.Lock()
		c.ErrLog = append(c.ErrLog, fmt.Sprintf("%s :: %s", time.Now().String(), msg))
		c.Unlock()
	}
}
