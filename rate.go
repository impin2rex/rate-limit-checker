package main

import (
	"bytes"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/impin2rex/rate-limit-checker/utils"
	log "github.com/sirupsen/logrus"
)

type Config struct {
	URL         string
	Method      string
	RPS         int
	Duration    int
	Body        string
	Headers     map[string]string
	WorkerCount int
	LogLevel    string
}

type jobInfo struct {
	requestNum int
	second     int
}

type Result struct {
	Timestamp    time.Time
	StatusCode   int
	ResponseTime time.Duration
	Error        error
	RequestStart time.Time
	Second       int
}

type Stats struct {
	TotalRequests     int
	SuccessRequests   int
	FailedRequests    int
	RateLimited       int
	AvgResponseTime   time.Duration
	MaxResponseTime   time.Duration
	MinResponseTime   time.Duration
	RequestsPerSecond float64
}

type SecondStats struct {
	Second        int
	Total         int
	Success       int
	RateLimited   int
	ClientErrors  int
	ServerErrors  int
	NetworkErrors int
}

var (
	urlFlag      = flag.String("u", "", "Target URL")
	methodFlag   = flag.String("X", "GET", "HTTP method")
	rpsFlag      = flag.Int("r", 200, "Requests per second")
	durationFlag = flag.Int("t", 10, "Test duration in seconds")
	bodyFlag     = flag.String("d", "", "Request body")
	headerFlag   = flag.String("h", "", "Headers in format 'Key:Value,Key2:Value2'")
	workerCount  = flag.Int("w", 50, "Number of concurrent workers")
	logLevel     = flag.String("v", "info", "Log Level")
)

func init() {
	flag.Parse()
	utils.SetupLogger(strings.ToUpper(*logLevel))
}

func main() {
	if *urlFlag == "" {
		log.Fatal("URL is required")
	}

	config := Config{
		URL:         *urlFlag,
		Method:      *methodFlag,
		RPS:         *rpsFlag,
		Duration:    *durationFlag,
		Body:        *bodyFlag,
		Headers:     parseHeaders(*headerFlag),
		WorkerCount: *workerCount,
	}

	fmt.Printf("Starting rate limit test...\n")
	fmt.Printf("URL: %s\n", config.URL)
	fmt.Printf("Method: %s\n", config.Method)
	fmt.Printf("Target RPS: %d\n", config.RPS)
	fmt.Printf("Duration: %d seconds\n", config.Duration)
	fmt.Printf("Body: %s\n", config.Body)
	fmt.Printf("Headers: %+v\n", config.Headers)
	fmt.Println(strings.Repeat("-", 50))

	results := runTest(config)
	stats := calculateStats(results, config.Duration)
	secondStats := calculateSecondStats(results, config.Duration)
	printResults(stats, secondStats)
}

func parseHeaders(headerStr string) map[string]string {
	headers := make(map[string]string)
	if headerStr == "" {
		return headers
	}

	pairs := strings.Split(headerStr, ",")
	for _, pair := range pairs {
		parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
		if len(parts) == 2 {
			headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return headers
}

func runTest(config Config) []Result {
	var results []Result
	var mu sync.Mutex
	var wg sync.WaitGroup

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			MaxIdleConnsPerHost: 1000,
		},
	}

	numWorkers := config.WorkerCount
	jobChan := make(chan jobInfo, config.RPS*10)

	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobChan {
				reqStart := time.Now()
				result := makeRequest(client, config)
				result.RequestStart = reqStart
				result.Second = job.second

				mu.Lock()
				results = append(results, result)
				mu.Unlock()

				status := "OK"
				if result.Error != nil {
					status = "ERROR"
				} else if result.StatusCode == 429 {
					status = "RATE LIMITED"
				} else if result.StatusCode >= 400 {
					status = fmt.Sprintf("HTTP %d", result.StatusCode)
				}

				log.Debugf("[%d] %s - %dms - %s\n",
					job.requestNum,
					result.Timestamp.Format("15:04:05.000"),
					result.ResponseTime.Milliseconds(),
					status)
			}
		}()
	}

	log.Infof("Sending requests at %d RPS for %d seconds using %d workers...\n", config.RPS, config.Duration, numWorkers)

	startTime := time.Now()
	requestCount := 0

	for second := 0; second < config.Duration; second++ {
		secondStartTime := startTime.Add(time.Duration(second) * time.Second)

		for req := 0; req < config.RPS; req++ {
			requestCount++

			requestOffset := time.Duration(req) * time.Second / time.Duration(config.RPS)
			targetTime := secondStartTime.Add(requestOffset)

			if sleepTime := time.Until(targetTime); sleepTime > 0 {
				time.Sleep(sleepTime)
			}

			select {
			case jobChan <- jobInfo{requestNum: requestCount, second: second + 1}:
			default:
				log.Warnf("Warning: Skipped request %d due to full worker queue\n", requestCount)
			}
		}

		log.Infof("Completed second %d: sent %d requests\n", second+1, config.RPS)
	}

	close(jobChan)

	log.Infof("Waiting for remaining requests to complete...")
	wg.Wait()

	totalTime := time.Since(startTime)
	log.Infof("Test completed in %v, sent exactly %d requests (%d per second)\n",
		totalTime, requestCount, config.RPS)
	return results
}

func makeRequest(client *http.Client, config Config) Result {
	start := time.Now()

	var bodyReader io.Reader
	if config.Body != "" {
		bodyReader = bytes.NewBufferString(config.Body)
	}

	req, err := http.NewRequest(config.Method, config.URL, bodyReader)
	if err != nil {
		return Result{
			Timestamp:    start,
			Error:        err,
			ResponseTime: time.Since(start),
		}
	}

	for key, value := range config.Headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	responseTime := time.Since(start)

	result := Result{
		Timestamp:    start,
		ResponseTime: responseTime,
		Error:        err,
	}

	if err != nil {
		return result
	}

	result.StatusCode = resp.StatusCode

	if resp.Body != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	return result
}

func calculateStats(results []Result, duration int) Stats {
	stats := Stats{
		TotalRequests:   len(results),
		MinResponseTime: time.Minute,
	}

	var totalResponseTime time.Duration

	for _, result := range results {
		if result.Error != nil {
			stats.FailedRequests++
			continue
		}

		if result.StatusCode == 429 {
			stats.RateLimited++
		}

		if result.StatusCode >= 200 && result.StatusCode < 300 {
			stats.SuccessRequests++
		} else {
			stats.FailedRequests++
		}

		totalResponseTime += result.ResponseTime

		if result.ResponseTime > stats.MaxResponseTime {
			stats.MaxResponseTime = result.ResponseTime
		}

		if result.ResponseTime < stats.MinResponseTime {
			stats.MinResponseTime = result.ResponseTime
		}
	}

	if stats.TotalRequests > 0 {
		stats.AvgResponseTime = totalResponseTime / time.Duration(stats.TotalRequests)
		stats.RequestsPerSecond = float64(stats.TotalRequests) / float64(duration)
	}

	if stats.MinResponseTime == time.Hour {
		stats.MinResponseTime = 0
	}

	return stats
}

func calculateSecondStats(results []Result, duration int) []SecondStats {
	secondStats := make([]SecondStats, duration)

	for i := range duration {
		secondStats[i].Second = i + 1
	}

	if len(results) == 0 {
		return secondStats
	}

	for _, result := range results {
		secondIndex := result.Second - 1

		if secondIndex < 0 || secondIndex >= duration {
			continue
		}

		secondStats[secondIndex].Total++

		if result.Error != nil {
			secondStats[secondIndex].NetworkErrors++
		} else if result.StatusCode == 429 {
			secondStats[secondIndex].RateLimited++
		} else if result.StatusCode >= 200 && result.StatusCode < 300 {
			secondStats[secondIndex].Success++
		} else if result.StatusCode >= 400 && result.StatusCode < 500 {
			secondStats[secondIndex].ClientErrors++
		} else if result.StatusCode >= 500 {
			secondStats[secondIndex].ServerErrors++
		}
	}

	return secondStats
}

func printResults(stats Stats, secondStats []SecondStats) {
	fmt.Println(strings.Repeat("=", 60))
	log.Info("RATE LIMIT TEST RESULTS")
	fmt.Println(strings.Repeat("=", 60))

	log.Infof("Total Requests:      %d\n", stats.TotalRequests)
	log.Infof("Successful:          %d (%.1f%%)\n",
		stats.SuccessRequests,
		float64(stats.SuccessRequests)/float64(stats.TotalRequests)*100)
	log.Infof("Failed:              %d (%.1f%%)\n",
		stats.FailedRequests,
		float64(stats.FailedRequests)/float64(stats.TotalRequests)*100)
	log.Infof("Rate Limited (429):  %d (%.1f%%)\n",
		stats.RateLimited,
		float64(stats.RateLimited)/float64(stats.TotalRequests)*100)

	fmt.Println(strings.Repeat("-", 60))
	log.Infof("Actual RPS:          %.2f\n", stats.RequestsPerSecond)
	log.Infof("Avg Response Time:   %v\n", stats.AvgResponseTime)
	log.Infof("Min Response Time:   %v\n", stats.MinResponseTime)
	log.Infof("Max Response Time:   %v\n", stats.MaxResponseTime)

	fmt.Println(strings.Repeat("-", 60))
	log.Info("PER-SECOND BREAKDOWN:")
	fmt.Println(strings.Repeat("-", 60))

	for _, secStats := range secondStats {
		others := secStats.Total - secStats.Success - secStats.RateLimited - secStats.NetworkErrors
		if secStats.Total > 0 {
			log.Infof("[%ds] Total: %d, Success: %d, RateLimited: %d, ClientErrors: %d, ServerErrors: %d",
				secStats.Second, secStats.Total, secStats.Success, secStats.RateLimited, secStats.ClientErrors, secStats.ServerErrors)
			if others > 0 {
				log.Infof(", Others: %d", others)
			}
			if secStats.NetworkErrors > 0 {
				log.Infof(", NetworkErrors: %d", secStats.NetworkErrors)
			}
			fmt.Println()
		} else {
			log.Infof("[%ds] Total: 0, Success: 0, RateLimited: 0\n", secStats.Second)
		}
	}

	fmt.Println(strings.Repeat("-", 60))

	if stats.RateLimited > 0 {
		log.Infof("⚠️  RATE LIMIT DETECTED: %d requests were rate limited\n", stats.RateLimited)
		successRate := float64(stats.SuccessRequests) / float64(stats.TotalRequests) * stats.RequestsPerSecond
		log.Infof("📊 Effective Success Rate: %.2f RPS\n", successRate)
	} else {
		log.Infof("✅ No rate limiting detected at %.2f RPS\n", stats.RequestsPerSecond)
	}
}
