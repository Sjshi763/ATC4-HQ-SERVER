package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
)

func init() {
	requestQueue = make(chan Request, queueSize)

	// Start request processor
	go processRequests()
}

func processRequests() {
	for req := range requestQueue {
		// Process request in a separate goroutine
		go func(r Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("Panic recovered in download handler: %v", rec)
				}
				r.done <- true
			}()

			// Add a small delay to prevent overwhelming
			time.Sleep(10 * time.Millisecond)

			downloadHandler(r.w, r.r)
			r.done <- true
		}(req)
	}
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	// Add nil checks
	if w == nil || r == nil {
		log.Printf("Nil request or response writer")
		return
	}

	// Set headers first before any potential writes
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Cache-Control", "no-cache")

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
	defer func() {
		if file != nil {
			file.Close()
		}
	}()

	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Set headers for large file download (must be set before any Write)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(fileName)))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.Header().Set("Accept-Ranges", "bytes")

	// Check if client disconnected using context
	ctx := r.Context()

	// Use smaller buffer for better memory management
	buffer := make([]byte, 32*1024) // 32KB buffer

	// Stream the file in chunks
	for {
		select {
		case <-ctx.Done():
			// Client disconnected, stop processing
			log.Printf("Client disconnected during download of %s", fileName)
			return
		default:
			// Check if file is still valid
			if file == nil {
				log.Printf("File handle is nil during download of %s", fileName)
				return
			}

			n, err := file.Read(buffer)
			if n > 0 {
				// Check if the connection is still alive before writing
				if w == nil {
					log.Printf("Response writer is nil during download of %s", fileName)
					return
				}

				if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
					log.Printf("Write error during download of %s: %v", fileName, writeErr)
					return
				}

				// Flush the response writer to ensure data is sent immediately
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}

			if err == io.EOF {
				break
			}

			if err != nil {
				log.Printf("Read error during download of %s: %v", fileName, err)
				return
			}
		}
	}

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
		// Wait for completion or timeout (increased to 20 minutes for large files)
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Minute)
		defer cancel()

		select {
		case <-done:
			// Request completed successfully
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				log.Printf("Request timeout for %s", r.URL.RawQuery)
				http.Error(w, "Request timeout", http.StatusRequestTimeout)
			} else {
				log.Printf("Request cancelled for %s", r.URL.RawQuery)
			}
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

	// Configure server with extended timeouts for large file downloads
	server := &http.Server{
		Addr:         ":8080",
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 600 * time.Second, // Increased to 10 minutes for large files
		IdleTimeout:  120 * time.Second,
		// Add connection keep-alive settings
		MaxHeaderBytes: 1 << 20, // 1 MB
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
