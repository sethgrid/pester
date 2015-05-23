# pester

`pester` wraps the standard lib to provide several options to help get your request data.
- Send out multiple requests and get the first back (only used for GET calls)
- Retry on errors
- Backoff

### Simple Example
Use `pester` where you would use the http client calls. By default, pester will use a concurrency of 1, and retry the endpoint 3 times with the `DefaultBackoff` strategy of waiting 1 second between retries.
```go
/* swap in replacement, just switch
   http.{Get|Post|PostForm|Head} to
   pester.{Get|Post|PostForm|Head}
*/
resp, err := pester.Get("http://sethammons.com")
```

### Complete example
For a complete and working example, see the sample directory.
`pester` allows you to use a constructor to control:
- backoff strategy
- reties
- concurrency
- keeping a log for debugging
```go
package main

import (
    "log"
    "net/http"
    "strings"

    "github.com/sethgrid/pester"
)

func main() {
    log.Println("Starting...")

    { // drop in replacement for http.Get and other client methods
        resp, err := pester.Get("http://example.com")
        if err != nil {
            log.Println("error GETing example.com", err)
        }
        defer resp.Body.Close()
        log.Printf("example.com %s", resp.Status)
    }

    { // control the resiliency
        client := pester.New()
        client.Concurrency = 3
        client.MaxRetries = 5
        client.Backoff = pester.ExponentialBackoff
        client.KeepLog = true

        resp, err := client.Get("http://example.com")
        if err != nil {
            log.Println("error GETing example.com", client.LogString())
        }
        defer resp.Body.Close()
        log.Printf("example.com %s", resp.Status)
    }

    { // use the pester version of http.Client.Do
        req, err := http.NewRequest("POST", "http://example.com", strings.NewReader("data"))
        if err != nil {
            log.Fatal("Unable to create a new http request", err)
        }
        resp, err := pester.Do(req)
        if err != nil {
            log.Println("error POSTing example.com", err)
        }
        defer resp.Body.Close()
        log.Printf("example.com %s", resp.Status)
    }
}

```

### Example Log
`pester` also allows you to control the resiliency and can optionally log the errors.
```go
c := pester.New()
c.KeepLog = true

nonExistantURL := "http://localhost:9000/foo"
_, _ = c.Get(nonExistantURL)

fmt.Println(c.LogString())
/*
Output:

1432402837 Get [GET] http://localhost:9000/foo request-0 retry-0 error: Get http://localhost:9000/foo: dial tcp 127.0.0.1:9000: connection refused
1432402838 Get [GET] http://localhost:9000/foo request-0 retry-1 error: Get http://localhost:9000/foo: dial tcp 127.0.0.1:9000: connection refused
1432402839 Get [GET] http://localhost:9000/foo request-0 retry-2 error: Get http://localhost:9000/foo: dial tcp 127.0.0.1:9000: connection refused
*/
```

![Are we there yet?](http://butchbellah.com/wp-content/uploads/2012/06/Are-We-There-Yet.jpg)

Are we there yet? Are we there yet? Are we there yet? Are we there yet? ...
