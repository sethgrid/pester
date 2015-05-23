package pester

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type Client struct {
	hc http.Client

	Concurrency int
	MaxRetries  int
	Backoff     BackoffStrategy
	KeepLog     bool

	sync.Mutex
	ErrLog []string
}

type result struct {
	resp *http.Response
	err  error
}

type params struct {
	method   string
	req      *http.Request
	url      string
	bodyType string
	body     io.Reader
	data     url.Values
}

func New() *Client {
	return &Client{Concurrency: 1, MaxRetries: 3, Backoff: DefaultBackoff, ErrLog: []string{}}
}

type BackoffStrategy func(retry int) time.Duration

func DefaultBackoff(_ int) time.Duration {
	return 1 * time.Second
}

func ExponentialBackoff(i int) time.Duration {
	return time.Duration(math.Pow(2, float64(i))) * time.Second
}

func (c *Client) pester(p params) (*http.Response, error) {
	resultCh := make(chan result)

	for req := 0; req < c.Concurrency; req++ {
		go func(n int, p params) {
			resp := &http.Response{}
			var err error

			for i := 0; i < c.MaxRetries; i++ {

				switch p.method {
				case "Do":
					resp, err = c.hc.Do(p.req)
				case "Get":
					resp, err = c.hc.Get(p.url)
				case "Head":
					resp, err = c.hc.Head(p.url)
				case "Post":
					resp, err = c.hc.Post(p.url, p.bodyType, p.body)
				case "PostForm":
					resp, err = c.hc.PostForm(p.url, p.data)
				}

				if err == nil && resp.StatusCode < 400 {
					resultCh <- result{resp: resp, err: err}
				}
				c.log(fmt.Sprintf("GET %s :: [req %d :: ret %d] :: %s", p.url, n, i, err.Error()))
				<-time.Tick(c.Backoff(i))
			}
			resultCh <- result{resp: resp, err: err}
		}(req, p)
	}

	for {
		select {
		case res := <-resultCh:
			return res.resp, res.err
		}
	}

	return nil, nil
}

func (c *Client) Do(req *http.Request) (resp *http.Response, err error) {
	return c.pester(params{method: "Do", req: req})
}

func (c *Client) Get(url string) (resp *http.Response, err error) {
	return c.pester(params{method: "Get", url: url})
}

func (c *Client) Head(url string) (resp *http.Response, err error) {
	return c.pester(params{method: "Head", url: url})
}

func (c *Client) Post(url string, bodyType string, body io.Reader) (resp *http.Response, err error) {
	return c.pester(params{method: "Post", url: url, bodyType: bodyType, body: body})
}

func (c *Client) PostForm(url string, data url.Values) (resp *http.Response, err error) {
	return c.pester(params{method: "PostForm", url: url, data: data})
}

func (c *Client) log(msg string) {
	if c.KeepLog {
		c.Lock()
		c.ErrLog = append(c.ErrLog, fmt.Sprintf("%s :: %s", time.Now().String(), msg))
		c.Unlock()
	}
}
