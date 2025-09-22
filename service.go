package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

const (
	downloadDir = "files"
)

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	fileName := r.URL.Query().Get("file")
	if fileName == "" {
		http.Error(w, "File name is required", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(downloadDir, filepath.Clean(fileName))

	// Security check to prevent directory traversal
	if !filepath.HasPrefix(filePath, downloadDir) {
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
}

func main() {
	// Create the download directory if it doesn't exist
	if _, err := os.Stat(downloadDir); os.IsNotExist(err) {
		os.Mkdir(downloadDir, 0755)
		fmt.Printf("Created directory '%s'\\n", downloadDir)
	}

	http.HandleFunc("/download", downloadHandler)

	port := "8080"
	fmt.Printf("Starting server on port %s...\\n", port)
	fmt.Printf("Use http://localhost:%s/download?file=<filename> to download a file.\\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Printf("Error starting server: %s\\n", err)
	}
}