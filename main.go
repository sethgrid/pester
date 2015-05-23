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
	// wrap it to acces
	hc http.Client

	// pester specific
	Concurrency int
	MaxRetries  int
	Backoff     BackoffStrategy
	KeepLog     bool

	sync.Mutex
	ErrLog []ErrEntry
}

type result struct {
	resp *http.Response
	err  error
}

type ErrEntry struct {
	Time    time.Time
	Method  string
	URL     string
	Verb    string
	Request int
	Retry   int
	Err     error
}

type params struct {
	method   string
	verb     string
	req      *http.Request
	url      string
	bodyType string
	body     io.Reader
	data     url.Values
}

func New() *Client {
	return &Client{Concurrency: 1, MaxRetries: 3, Backoff: DefaultBackoff, ErrLog: []ErrEntry{}}
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

				c.log(ErrEntry{
					Time:    time.Now(),
					Method:  p.method,
					Verb:    p.verb,
					URL:     p.url,
					Request: n,
					Retry:   i,
					Err:     err,
				})

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

func (c *Client) LogString() string {
	var res string
	for _, e := range c.ErrLog {
		res += fmt.Sprintf("%d %s [%s] %s request-%d retry-%d error: %s\n",
			e.Time.Unix(), e.Method, e.Verb, e.URL, e.Request, e.Retry, e.Err)
	}
	return res
}

func (c *Client) log(e ErrEntry) {
	if c.KeepLog {
		c.Lock()
		c.ErrLog = append(c.ErrLog, e)
		c.Unlock()
	}
}

func (c *Client) Do(req *http.Request) (resp *http.Response, err error) {
	return c.pester(params{method: "Do", req: req, verb: req.Method, url: req.URL.String()})
}

func (c *Client) Get(url string) (resp *http.Response, err error) {
	return c.pester(params{method: "Get", url: url, verb: "GET"})
}

func (c *Client) Head(url string) (resp *http.Response, err error) {
	return c.pester(params{method: "Head", url: url, verb: "HEAD"})
}

func (c *Client) Post(url string, bodyType string, body io.Reader) (resp *http.Response, err error) {
	return c.pester(params{method: "Post", url: url, bodyType: bodyType, body: body, verb: "POST"})
}

func (c *Client) PostForm(url string, data url.Values) (resp *http.Response, err error) {
	return c.pester(params{method: "PostForm", url: url, data: data, verb: "POST"})
}

////////////////////////////////////////
// Provide self-constructing variants //
////////////////////////////////////////

func Do(req *http.Request) (resp *http.Response, err error) {
	c := New()
	return c.Do(req)
}

func Get(url string) (resp *http.Response, err error) {
	c := New()
	return c.Get(url)
}

func Head(url string) (resp *http.Response, err error) {
	c := New()
	return c.Head(url)
}

func Post(url string, bodyType string, body io.Reader) (resp *http.Response, err error) {
	c := New()
	return c.Post(url, bodyType, body)
}

func PostForm(url string, data url.Values) (resp *http.Response, err error) {
	c := New()
	return c.PostForm(url, data)
}
