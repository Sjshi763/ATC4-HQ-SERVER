package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	downloadDir = "files"
	maxWorkers  = 100
	queueSize   = 1000
)

type Request struct {
	w    http.ResponseWriter
	r    *http.Request
	done chan bool
}

var (
	requestQueue chan Request
	workerPool   chan chan Request
	wg           sync.WaitGroup
)

func init() {
	requestQueue = make(chan Request, queueSize)
	workerPool = make(chan chan Request, maxWorkers)

	// Start worker pool
	for i := 0; i < maxWorkers; i++ {
		go worker()
	}

	// Start request processor
	go processRequests()
}

func worker() {
	defer wg.Done()

	// Register this worker
	workerPool <- requestQueue

	for req := range requestQueue {
		// Process the request in a separate goroutine
		go func(r Request) {
			downloadHandler(r.w, r.r)
			r.done <- true
		}(req)
	}
}

func processRequests() {
	for req := range requestQueue {
		workerChan := <-workerPool
		workerChan <- req
	}
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	log.Printf("Starting download request for %s", r.URL.RawQuery)

	fileName := r.URL.Query().Get("file")
	if fileName == "" {
		http.Error(w, "File name is required", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(downloadDir, filepath.Clean(fileName))

	// Security check to prevent directory traversal
	absDownloadDir, err := filepath.Abs(downloadDir)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if len(absFilePath) < len(absDownloadDir) || absFilePath[:len(absDownloadDir)] != absDownloadDir {
		http.Error(w, "Invalid file path", http.StatusBadRequest)
		return
	}

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(fileName)))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))

	http.ServeContent(w, r, stat.Name(), stat.ModTime(), file)

	log.Printf("Completed download request for %s in %v", fileName, time.Since(startTime))
}

func queuedDownloadHandler(w http.ResponseWriter, r *http.Request) {
	done := make(chan bool)
	req := Request{
		w:    w,
		r:    r,
		done: done,
	}

	// Try to queue the request
	select {
	case requestQueue <- req:
		// Wait for completion or timeout
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		select {
		case <-done:
			// Request completed successfully
		case <-ctx.Done():
			http.Error(w, "Request timeout", http.StatusRequestTimeout)
		}
	default:
		// Queue is full
		http.Error(w, "Server busy, please try again later", http.StatusServiceUnavailable)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status": "ok", "workers": %d, "queue_size": %d}`, maxWorkers, len(requestQueue))
}

func main() {
	// Create the download directory if it doesn't exist
	if _, err := os.Stat(downloadDir); os.IsNotExist(err) {
		if err := os.Mkdir(downloadDir, 0755); err != nil {
			log.Fatalf("Failed to create download directory: %v", err)
		}
		fmt.Printf("Created directory '%s'\n", downloadDir)
	}

	// Configure server with timeouts
	server := &http.Server{
		Addr:         ":8080",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Register handlers
	http.HandleFunc("/download", queuedDownloadHandler)
	http.HandleFunc("/health", healthHandler)

	fmt.Printf("Starting server on port 8080...\n")
	fmt.Printf("Use http://localhost:8080/download?file=<filename> to download a file.\n")
	fmt.Printf("Use http://localhost:8080/health to check server status.\n")

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Error starting server: %s\n", err)
	}
}
