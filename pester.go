// Package pester provides additional resiliency over the standard http client methods by
// allowing you to control concurrency, retries, and a backoff strategy.
package pester

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	methodDo                  = "Do"
	methodGet                 = "Get"
	methodHead                = "Head"
	methodPost                = "Post"
	methodPostForm            = "PostForm"
	headerKeyContentType      = "Content-Type"
	contentTypeFormURLEncoded = "application/x-www-form-urlencoded"
)

//ErrUnexpectedMethod occurs when an http.Client method is unable to be mapped from a calling method in the pester client
var ErrUnexpectedMethod = errors.New("unexpected client method, must be one of Do, Get, Head, Post, or PostFrom")

// ErrReadingBody happens when we cannot read the body bytes
// Deprecated: use ErrReadingRequestBody
var ErrReadingBody = errors.New("error reading body")

// ErrReadingRequestBody happens when we cannot read the request body bytes
var ErrReadingRequestBody = errors.New("error reading request body")

// Client wraps the http client and exposes all the functionality of the http.Client.
// Additionally, Client provides pester specific values for handling resiliency.
type Client struct {
	// wrap it to provide access to http built ins
	hc *http.Client

	Transport     http.RoundTripper
	CheckRedirect func(req *http.Request, via []*http.Request) error
	Jar           http.CookieJar
	Timeout       time.Duration

	// pester specific
	Concurrency    int
	MaxRetries     int
	Backoff        BackoffStrategy
	KeepLog        bool
	LogHook        LogHook
	ContextLogHook ContextLogHook

	SuccessReqNum   int
	SuccessRetryNum int

	wg *sync.WaitGroup

	sync.Mutex
	ErrLog         []ErrEntry
	RetryOnHTTP429 bool
}

// ErrEntry is used to provide the LogString() data and is populated
// each time an error happens if KeepLog is set.
// ErrEntry.Retry is deprecated in favor of ErrEntry.Attempt
type ErrEntry struct {
	Time    time.Time
	Method  string
	URL     string
	Verb    string
	Request int
	Retry   int
	Attempt int
	Err     error
}

// result simplifies the channel communication for concurrent request handling
type result struct {
	resp  *http.Response
	err   error
	req   int
	retry int
}

// params represents all the params needed to run http client calls and pester errors
type params struct {
	method   string
	verb     string
	req      *http.Request
	url      string
	bodyType string
	body     io.ReadCloser
	data     url.Values
}

var random *rand.Rand

func init() {
	random = rand.New(rand.NewSource(time.Now().UnixNano()))
}

// New constructs a new DefaultClient with sensible default values
func New() *Client {
	return &Client{
		Concurrency:    DefaultClient.Concurrency,
		MaxRetries:     DefaultClient.MaxRetries,
		Backoff:        DefaultClient.Backoff,
		ErrLog:         DefaultClient.ErrLog,
		wg:             &sync.WaitGroup{},
		RetryOnHTTP429: false,
	}
}

// NewExtendedClient allows you to pass in an http.Client that is previously set up
// and extends it to have Pester's features of concurrency and retries.
func NewExtendedClient(hc *http.Client) *Client {
	c := New()
	c.hc = hc
	return c
}

// LogHook is used to log attempts as they happen. This function is never called,
// however, if KeepLog is set to true.
type LogHook func(e ErrEntry)

// ContextLogHook does the same as LogHook but with passed Context
type ContextLogHook func(ctx context.Context, e ErrEntry)

// BackoffStrategy is used to determine how long a retry request should wait until attempted
type BackoffStrategy func(retry int) time.Duration

// DefaultClient provides sensible defaults
var DefaultClient = &Client{Concurrency: 1, MaxRetries: 3, Backoff: DefaultBackoff, ErrLog: []ErrEntry{}}

// DefaultBackoff always returns 1 second
func DefaultBackoff(_ int) time.Duration {
	return 1 * time.Second
}

// ExponentialBackoff returns ever increasing backoffs by a power of 2
func ExponentialBackoff(i int) time.Duration {
	return time.Duration(1<<uint(i)) * time.Second
}

// ExponentialJitterBackoff returns ever increasing backoffs by a power of 2
// with +/- 0-33% to prevent sychronized reuqests.
func ExponentialJitterBackoff(i int) time.Duration {
	return jitter(int(1 << uint(i)))
}

// LinearBackoff returns increasing durations, each a second longer than the last
func LinearBackoff(i int) time.Duration {
	return time.Duration(i) * time.Second
}

// LinearJitterBackoff returns increasing durations, each a second longer than the last
// with +/- 0-33% to prevent sychronized reuqests.
func LinearJitterBackoff(i int) time.Duration {
	return jitter(i)
}

// jitter keeps the +/- 0-33% logic in one place
func jitter(i int) time.Duration {
	ms := i * 1000

	maxJitter := ms / 3

	// ms ± rand
	ms += random.Intn(2*maxJitter) - maxJitter

	// a jitter of 0 messes up the time.Tick chan
	if ms <= 0 {
		ms = 1
	}

	return time.Duration(ms) * time.Millisecond
}

// Wait blocks until all pester requests have returned
// Probably not that useful outside of testing.
func (c *Client) Wait() {
	c.wg.Wait()
}

func (c *Client) copyBody(src io.ReadCloser) ([]byte, error) {
	b, err := ioutil.ReadAll(src)
	if err != nil {
		return nil, ErrReadingRequestBody
	}
	src.Close()

	return b, nil
}

// resetBody resets the Body and GetBody fields of an http.Request to new Readers over
// the originalBody. This is used to refresh http.Requests that may have had their
// bodies closed already.
func resetBody(request *http.Request, originalBody []byte) {
	request.Body = io.NopCloser(bytes.NewBuffer(originalBody))
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewBuffer(originalBody)), nil
	}
}

// pester provides all the logic of retries, concurrency, backoff, and logging
func (c *Client) pester(p params) (*http.Response, error) {
	resultCh := make(chan result)
	multiplexCh := make(chan result)
	finishCh := make(chan struct{})

	// track all requests that go out so we can close the late listener routine that closes late incoming response bodies
	totalSentRequests := &sync.WaitGroup{}
	totalSentRequests.Add(1)
	defer totalSentRequests.Done()
	allRequestsBackCh := make(chan struct{})
	go func() {
		totalSentRequests.Wait()
		close(allRequestsBackCh)
	}()

	// GET calls should be idempotent and can make use
	// of concurrency. Other verbs can mutate and should not
	// make use of the concurrency feature
	concurrency := c.Concurrency
	if p.verb != http.MethodGet {
		concurrency = 1
	}

	c.Lock()
	if c.hc == nil {
		c.hc = &http.Client{}
		c.hc.Transport = c.Transport
		c.hc.CheckRedirect = c.CheckRedirect
		c.hc.Jar = c.Jar
		c.hc.Timeout = c.Timeout
	}
	c.Unlock()

	// re-create the http client so we can leverage the std lib
	httpClient := http.Client{
		Transport:     c.hc.Transport,
		CheckRedirect: c.hc.CheckRedirect,
		Jar:           c.hc.Jar,
		Timeout:       c.hc.Timeout,
	}

	// if we have a request body, we need to save it for later
	var (
		originalBody []byte
		err          error
	)

	if p.req != nil && p.req.Body != nil && p.body == nil {
		originalBody, err = c.copyBody(p.req.Body)
	} else if p.body != nil {
		originalBody, err = c.copyBody(p.body)
	}
	if err != nil {
		return nil, err
	}

	// check to make sure that we aren't trying to use an unsupported method
	switch p.method {
	case methodDo, methodGet, methodHead, methodPostForm, methodPost:
	default:
		return nil, ErrUnexpectedMethod
	}

	// provideRequest returns an HTTP request to be use when retrying.
	// if concurrency is 1, it will return the same request that was supplied to the Do() method
	// for Do() calls, otherwise it will generate a Clone() of the request each time it is called.
	// For non-Do() calls, it creates a new request each time it is called. This re-creation behaviour
	// is because requests are not supposed to be used again until the RoundTripper is finished
	// with them, which cannot be guaranteed with concurrent callers
	// https://pkg.go.dev/net/http#RoundTripper
	provideRequest := func() (request *http.Request, err error) {
		switch p.method {
		case methodDo:
			if concurrency > 1 {
				request = p.req.Clone(p.req.Context())
			} else {
				request = p.req
			}
			if request.Body != nil {
				// reset the body since Clone() doesn't do that for us
				// and we drained it earlier when performing the Copy
				// ex: https://go.dev/play/p/jlc6A-fjaOi
				resetBody(request, originalBody)
			}
		case methodGet, methodHead:
			request, err = http.NewRequest(p.verb, p.url, nil)
		case methodPostForm, methodPost:
			request, err = http.NewRequest(http.MethodPost, p.url, bytes.NewBuffer(originalBody))
		}
		if err != nil {
			return
		}

		if len(p.bodyType) > 0 {
			request.Header.Set(headerKeyContentType, p.bodyType)
		}

		return
	}

	AttemptLimit := c.MaxRetries
	if AttemptLimit <= 0 {
		AttemptLimit = 1
	}

	for n := 0; n < concurrency; n++ {
		c.wg.Add(1)
		totalSentRequests.Add(1)
		go func(n int) {
			defer c.wg.Done()
			defer totalSentRequests.Done()
			req, err := provideRequest()
			// couldn't get a request to use, so don't proceed
			if err != nil {
				multiplexCh <- result{err: err, req: n}
				return
			}

			for i := 1; i <= AttemptLimit; i++ {
				c.wg.Add(1)
				defer c.wg.Done()

				select {
				case <-finishCh:
					return
				default:
				}

				resp, err := httpClient.Do(req)
				// Early return if we have a valid result
				// Only retry (ie, continue the loop) on 5xx status codes and 429
				if err == nil && resp.StatusCode < http.StatusInternalServerError && (resp.StatusCode != http.StatusTooManyRequests || (resp.StatusCode == http.StatusTooManyRequests && !c.RetryOnHTTP429)) {
					multiplexCh <- result{resp: resp, err: err, req: n, retry: i}
					return
				}

				loggingContext := req.Context()
				c.log(
					loggingContext,
					ErrEntry{
						Time:    time.Now(),
						Method:  p.method,
						Verb:    req.Method,
						URL:     req.URL.String(),
						Request: n,
						Retry:   i + 1, // would remove, but would break backward compatibility
						Attempt: i,
						Err:     err,
					},
				)

				// if it is the last iteration, grab the result (which is an error at this point)
				if i == AttemptLimit {
					multiplexCh <- result{resp: resp, err: err}
					return
				}

				//If the request has been cancelled, skip retries
				select {
				case <-req.Context().Done():
					multiplexCh <- result{resp: resp, err: req.Context().Err()}
					return
				default:
				}

				// if we are retrying, we should close this response body to free the fd
				if resp != nil {
					resp.Body.Close()
				}

				select {
				// prevent a 0 from causing the tick to block, pass additional microsecond
				case <-time.After(c.Backoff(i) + 1*time.Microsecond):
				// allow context cancellation to cancel during backoff
				case <-req.Context().Done():
					multiplexCh <- result{resp: resp, err: req.Context().Err()}
					return
				}

				// we are about to retry, if we had a Body, we will need to restore it
				// to a non-closed one in order to work reliably. If you do not do this,
				// there are a number of curious edge cases depending on the type of the
				// underlying reader: https://go.dev/play/p/gZLVUe2EXSE
				if req.Body != nil {
					resetBody(req, originalBody)
				}
			}
		}(n)
	}

	// spin off the go routine so it can continually listen in on late results and close the response bodies
	go func() {
		gotFirstResult := false
		for {
			select {
			case res := <-multiplexCh:
				if !gotFirstResult {
					gotFirstResult = true
					close(finishCh)
					resultCh <- res
				} else if res.resp != nil {
					// we only return one result to the caller; close all other response bodies that come back
					// drain the body before close as to not prevent keepalive. see https://gist.github.com/mholt/eba0f2cc96658be0f717
					io.Copy(ioutil.Discard, res.resp.Body)
					res.resp.Body.Close()
				}
			case <-allRequestsBackCh:
				// don't leave this goroutine running
				return
			}
		}
	}()

	res := <-resultCh
	c.Lock()
	defer c.Unlock()
	c.SuccessReqNum = res.req
	c.SuccessRetryNum = res.retry

	return res.resp, res.err
}

// LogString provides a string representation of the errors the client has seen
func (c *Client) LogString() string {
	c.Lock()
	defer c.Unlock()
	var res string
	for _, e := range c.ErrLog {
		res += c.FormatError(e)
	}
	return res
}

// Format the Error to human readable string
func (c *Client) FormatError(e ErrEntry) string {
	return fmt.Sprintf("%d %s [%s] %s request-%d retry-%d error: %s\n",
		e.Time.Unix(), e.Method, e.Verb, e.URL, e.Request, e.Retry, e.Err)
}

// LogErrCount is a helper method used primarily for test validation
func (c *Client) LogErrCount() int {
	c.Lock()
	defer c.Unlock()
	return len(c.ErrLog)
}

// EmbedHTTPClient allows you to extend an existing Pester client with an
// underlying http.Client, such as https://godoc.org/golang.org/x/oauth2/google#DefaultClient
func (c *Client) EmbedHTTPClient(hc *http.Client) {
	c.hc = hc
}

func (c *Client) log(ctx context.Context, e ErrEntry) {
	if c.KeepLog {
		c.Lock()
		defer c.Unlock()
		c.ErrLog = append(c.ErrLog, e)
	} else if c.ContextLogHook != nil {
		// NOTE: There is a possibility that Log Printing hook slows it down.
		// but the consumer can always do the Job in a go-routine.
		c.ContextLogHook(ctx, e)
	} else if c.LogHook != nil {
		// NOTE: There is a possibility that Log Printing hook slows it down.
		// but the consumer can always do the Job in a go-routine.
		c.LogHook(e)
	}
}

// Do provides the same functionality as http.Client.Do
func (c *Client) Do(req *http.Request) (resp *http.Response, err error) {
	return c.pester(params{method: methodDo, req: req, verb: req.Method, url: req.URL.String()})
}

// Get provides the same functionality as http.Client.Get
func (c *Client) Get(url string) (resp *http.Response, err error) {
	return c.pester(params{method: methodGet, url: url, verb: http.MethodGet})
}

// Head provides the same functionality as http.Client.Head
func (c *Client) Head(url string) (resp *http.Response, err error) {
	return c.pester(params{method: methodHead, url: url, verb: http.MethodHead})
}

// Post provides the same functionality as http.Client.Post
func (c *Client) Post(url string, bodyType string, body io.Reader) (resp *http.Response, err error) {
	return c.pester(params{method: methodPost, url: url, bodyType: bodyType, body: ioutil.NopCloser(body), verb: http.MethodPost})
}

// PostForm provides the same functionality as http.Client.PostForm
func (c *Client) PostForm(url string, data url.Values) (resp *http.Response, err error) {
	return c.pester(params{method: methodPostForm, url: url, bodyType: contentTypeFormURLEncoded, body: ioutil.NopCloser(strings.NewReader(data.Encode())), verb: http.MethodPost})
}

// set RetryOnHTTP429 for clients,
func (c *Client) SetRetryOnHTTP429(flag bool) {
	c.RetryOnHTTP429 = flag
}

////////////////////////////////////////
// Provide self-constructing variants //
////////////////////////////////////////

// Do provides the same functionality as http.Client.Do and creates its own constructor
func Do(req *http.Request) (resp *http.Response, err error) {
	c := New()
	return c.Do(req)
}

// Get provides the same functionality as http.Client.Get and creates its own constructor
func Get(url string) (resp *http.Response, err error) {
	c := New()
	return c.Get(url)
}

// Head provides the same functionality as http.Client.Head and creates its own constructor
func Head(url string) (resp *http.Response, err error) {
	c := New()
	return c.Head(url)
}

// Post provides the same functionality as http.Client.Post and creates its own constructor
func Post(url string, bodyType string, body io.Reader) (resp *http.Response, err error) {
	c := New()
	return c.Post(url, bodyType, body)
}

// PostForm provides the same functionality as http.Client.PostForm and creates its own constructor
func PostForm(url string, data url.Values) (resp *http.Response, err error) {
	c := New()
	return c.PostForm(url, data)
}
