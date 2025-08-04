package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

func main() {
	// Define command-line flags
	url := flag.String("url", "", "Target API URL (required)")
	method := flag.String("method", "POST", "HTTP method (GET, POST, etc.)")
	body := flag.String("body", "", "Request body for POST/PUT")
	totalReq := flag.Int("requests", 100, "Total number of requests to send")
	headers := flag.String("header", "", "Comma-separated list of custom headers (e.g., Content-Type:application/json,Authorization:Bearer token)")

	flag.Parse()

	if *url == "" {
		fmt.Println("Error: --url is required")
		flag.Usage()
		return
	}

	// Parse headers
	headerMap := make(map[string]string)
	if *headers != "" {
		pairs := strings.Split(*headers, ",")
		for _, pair := range pairs {
			kv := strings.SplitN(pair, ":", 2)
			if len(kv) == 2 {
				headerMap[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			}
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	rateLimitHit := false

	start := time.Now()

	// Fire concurrent requests
	for i := 1; i <= *totalReq; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			var req *http.Request
			var err error

			if *method == "GET" || *method == "DELETE" {
				req, err = http.NewRequest(*method, *url, nil)
			} else {
				req, err = http.NewRequest(*method, *url, bytes.NewBuffer([]byte(*body)))
			}

			if err != nil {
				fmt.Printf("Request %d: Error creating request: %v\n", i, err)
				return
			}

			// Add headers
			for k, v := range headerMap {
				req.Header.Set(k, v)
			}

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("Request %d: Error: %v\n", i, err)
				return
			}
			defer resp.Body.Close()
			responseBody, _ := io.ReadAll(resp.Body)

			if resp.StatusCode == 200 {
				mu.Lock()
				successCount++
				mu.Unlock()
				fmt.Printf("Request %d: Success (Status: %d)\n", i, resp.StatusCode)
			} else {
				fmt.Printf("Request %d: Status: %d, Body: %s\n", i, resp.StatusCode, string(responseBody))
				if resp.StatusCode == 429 {
					mu.Lock()
					rateLimitHit = true
					mu.Unlock()
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	// Summary
	fmt.Printf("\n==== Summary ====\n")
	fmt.Printf("Total successful requests: %d\n", successCount)
	fmt.Printf("Rate limit hit? %v\n", rateLimitHit)
	fmt.Printf("Test duration: %s\n", elapsed)
}
