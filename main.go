// Package main "forwards" one SSE stream to another.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

var (
	agentKey string
	agentURL string
)

func main() {
	// Read the environment variables.
	if v := os.Getenv("AGENT_KEY"); v != "" {
		agentKey = v
	} else {
		log.Fatalln("An 'AGENT_KEY' environment variable is required")
	}

	if v := os.Getenv("AGENT_URL"); v != "" {
		agentURL = v
	} else {
		log.Println("No 'AGENT_URL' environment variable set, defaulting to Ben's test agent URL")
		agentURL = "https://bbknmramomyuvt64i6qbhqbe.agents.do-ai.run/api/v1/chat/completions"
	}

	// Serve the index.html file to observe how this looks on the frontend.
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	// Massive handler for the SSE forwarding.
	http.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		type msg struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}

		params := &struct {
			Message []*msg  `json:"messages"`
			TopP    float64 `json:"top_p"`
			Stream  bool    `json:"stream"`
		}{
			TopP:    0.02,
			Message: []*msg{{Role: "user", Content: "How do I create a Droplet?"}},
			Stream:  true,
		}

		data, err := json.Marshal(params)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, agentURL, bytes.NewReader(data))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		req.Header.Set("Authorization", "Bearer "+agentKey)
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Connection", "keep-alive")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for k, v := range resp.Header {
			w.Header().Set(k, v[0])
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Split(splitDoubleNewline)

		for scanner.Scan() {
			data := scanner.Bytes()
			w.Write(data)

			// Flush if it's supported, which it had better be.
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			if bytes.Contains(data, []byte("[DONE]")) {
				// Ensure the underlying connection is closed.
				if err := resp.Body.Close(); err != nil {
					log.Println("[ERROR] connection close... failed??", err)
				}
				return
			}
		}

		if err := scanner.Err(); err != nil {
			log.Println("[ERROR] failed to scan:", err)
			if err := resp.Body.Close(); err != nil {
				log.Println("[ERROR] connection close... failed??", err)
			}
		}
	})

	log.Println("Starting server on http://localhost:3000")
	http.ListenAndServe(":3000", nil)
}

func splitDoubleNewline(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	// Check for two consecutive newline characters.
	if i := bytes.Index(data, []byte{'\n', '\n'}); i >= 0 {
		return i + 2, data[0 : i+2], nil
	}

	// If we're at EOF, we have a final, non-terminated line. Return it.
	if atEOF {
		return len(data), data, nil
	}
	// Request more data.
	return 0, nil, nil
}
