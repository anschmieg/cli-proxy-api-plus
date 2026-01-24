package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
)

var (
	port          = flag.Int("port", 8081, "Mock server port")
	rateLimitMode = flag.Bool("ratelimit", false, "Enable rate limit simulation (429s)")
)

// RequestLog captures details of received requests for assertion.
type RequestLog struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers"`
	Body    string              `json:"body"`
}

var (
	mu       sync.Mutex
	requests []RequestLog
)

func main() {
	flag.Parse()

	http.HandleFunc("/v1internal:generateContent", handleCloudCode)
	http.HandleFunc("/v1internal:streamGenerateContent", handleCloudCodeStream)
	http.HandleFunc("/v1/chat/completions", handlePortkey)
	http.HandleFunc("/logs", handleLogs)
	http.HandleFunc("/reset", handleReset)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Mock Upstream Server listening on %s", addr)
	if *rateLimitMode {
		log.Println("Rate limit mode ENABLED")
	}
	log.Fatal(http.ListenAndServe(addr, nil))
}

func logRequest(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	// Restore body for handler
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	mu.Lock()
	defer mu.Unlock()
	requests = append(requests, RequestLog{
		Method:  r.Method,
		URL:     r.URL.String(),
		Headers: r.Header,
		Body:    string(body),
	})
	log.Printf("Received %s %s", r.Method, r.URL.Path)
}

func handleCloudCode(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	if *rateLimitMode {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error": {"code": 429, "message": "Rate limit exceeded"}}`))
		return
	}

	// Simple mock response
	response := `{"candidates": [{"content": {"parts": [{"text": "Mock CloudCode Response"}]}}]}`
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(response))
}

func handleCloudCodeStream(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	if *rateLimitMode {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error": {"code": 429, "message": "Rate limit exceeded"}}`))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	data := `{"candidates": [{"content": {"parts": [{"text": "Mock Stream Chunk"}]}}]}`
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func handlePortkey(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	if *rateLimitMode {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error": {"message": "Rate limit exceeded", "type": "rate_limit_error"}}`))
		return
	}

	response := `{"id": "chatcmpl-mock", "choices": [{"message": {"role": "assistant", "content": "Mock Portkey Response"}}]}`
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(response))
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(requests)
}

func handleReset(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	requests = nil
	w.WriteHeader(http.StatusOK)
}
