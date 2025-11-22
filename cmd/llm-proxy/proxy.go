package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	flag "github.com/spf13/pflag"
)

func main() {
	// Define and parse command line flags
	port := flag.IntP("port", "p", 8080, "Port to listen on (default: 8080)")
	targetAddr := flag.StringP("target", "t", "http://localhost:11434", "Target address to connect to (default: http://localhost:11434)")
	flag.Parse()

	// Validate target address format
	targetRegex := regexp.MustCompile(`^https?://[a-zA-Z0-9.-]+:[0-9]+$`)
	if !targetRegex.MatchString(*targetAddr) {
		log.Fatalf("Invalid target address format: %s. Must be in format 'http[s]://hostname:port'", *targetAddr)
	}

	// Extract and validate target port
	targetPortStr := strings.Split(*targetAddr, ":")[len(strings.Split(*targetAddr, ":"))-1]
	targetPort, err := strconv.Atoi(targetPortStr)
	if err != nil {
		log.Fatalf("Invalid target port: %s. Port must be a number", targetPortStr)
	}
	if targetPort < 1 || targetPort > 65535 {
		log.Fatalf("Invalid target port: %d. Port must be between 1 and 65535", targetPort)
	}

	// Validate listening port number is in valid range
	if *port < 1 || *port > 65535 {
		log.Fatalf("Invalid port number: %d. Port must be between 1 and 65535", *port)
	}

	// Create a custom transport with timeouts
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	// Create reverse proxy
	proxy := &http.Server{
		Addr: fmt.Sprintf("localhost:%d", *port),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create the target URL
			targetURL := *targetAddr + r.URL.Path
			if r.URL.RawQuery != "" {
				targetURL += "?" + r.URL.RawQuery
			}

			// Create new request
			proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
			if err != nil {
				http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
				return
			}

			// Copy headers
			for name, values := range r.Header {
				for _, value := range values {
					proxyReq.Header.Add(name, value)
				}
			}

			// Set the correct Host header
			proxyReq.Host = strings.TrimPrefix(*targetAddr, "http://")
			proxyReq.Host = strings.TrimPrefix(proxyReq.Host, "https://")

			// Make the request
			client := &http.Client{
				Transport: transport,
				Timeout:   30 * time.Second,
			}

			resp, err := client.Do(proxyReq)
			if err != nil {
				http.Error(w, "Error forwarding request: "+err.Error(), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()

			// Copy response headers
			for name, values := range resp.Header {
				for _, value := range values {
					w.Header().Add(name, value)
				}
			}

			// Set status code
			w.WriteHeader(resp.StatusCode)

			handleStreamingResponse(resp.Body, w)
		}),
	}

	log.Printf("Proxy server listening on %s", proxy.Addr)
	log.Printf("Forwarding requests to %s", *targetAddr)

	if err := proxy.ListenAndServe(); err != nil {
		log.Fatalf("Error starting proxy server: %v", err)
	}
}

func handleRegularResponse(body io.ReadCloser, w http.ResponseWriter) {
	// Read the entire response
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		log.Printf("Error reading response body: %v", err)
		return
	}

	// Try to parse as JSON and extract model answer
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(bodyBytes, &result); err == nil && len(result.Choices) > 0 {
		modelAnswer := result.Choices[0].Message.Content
		log.Printf("\n\n- - -\n\n%s", modelAnswer)
	} else {
		log.Printf("\n\n- - -\n\n%s", string(bodyBytes))
	}

	// Write the original response to client
	w.Write(bodyBytes)
}

func handleStreamingResponse(body io.ReadCloser, w http.ResponseWriter) {
	// Use a tee reader to both process and forward the stream
	pr, pw := io.Pipe()
	tee := io.TeeReader(body, pw)

	// Process the stream in a goroutine
	go func() {
		fmt.Printf("- - -\n")
		log.Printf("\n\n")
		defer pw.Close()
		scanner := bufio.NewScanner(tee)
		for scanner.Scan() {
			line := scanner.Bytes()

			// Try to parse each line as a JSON object
			if len(line) > 0 && (line[0] == '{' || strings.HasPrefix(string(line), "data: {")) {
				// Handle both regular JSON and SSE format
				jsonLine := line
				if strings.HasPrefix(string(line), "data: ") {
					jsonLine = line[6:] // Remove "data: " prefix
				}

				var result struct {
					Choices []struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
						Message struct {
							Content string `json:"content"`
						} `json:"message"`
					} `json:"choices"`
				}

				if err := json.Unmarshal(jsonLine, &result); err == nil && len(result.Choices) > 0 {
					// Check for delta content (streaming) or message content (non-streaming)
					content := result.Choices[0].Delta.Content
					if content == "" {
						content = result.Choices[0].Message.Content
					}
					if content != "" {
						fmt.Printf("%s", content)
					}
				}
			}
		}
		fmt.Print("\n\n")

		if err := scanner.Err(); err != nil {
			log.Printf("Error scanning stream: %v", err)
		}
	}()

	// Copy the processed stream to the response writer
	_, err := io.Copy(w, pr)
	if err != nil {
		log.Printf("Error copying stream: %v", err)
	}
}
