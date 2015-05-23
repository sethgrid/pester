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
