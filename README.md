# pester

Pester wraps the standard lib to provide several options to help get your request data.
- Send out multiple requests and get the first back (only used for GET calls)
- Retry on errors
- Backoff

### Example
Use `pester` where you would use the http client calls. By default, pester will use a concurrency of 1, and retry the endpoint 3 times with the `DefaultBackoff` strategy of waiting 1 second between retries.
```go
    /* swap in replacement, just switch
       http.{Get|Post|PostForm|Head} to
       pester.{Get|Post|PostForm|Head}
    */
    resp, err := pester.Get("http://sethammons.com")
```

### Example Log
`pester` also allows you to control the resiliency and can optionally log the errors.
```go
    /* use a constructor to control:
       - backoff strategy
       - reties
       - concurreny
       - keeping a log for debugging
    */
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
