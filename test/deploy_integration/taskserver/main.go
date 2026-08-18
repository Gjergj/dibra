package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type server struct {
	mu       sync.Mutex
	queueDir string
	logPath  string
}

type taskOutcome struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	queueDir := flag.String("queue-dir", "/tmp/dibra-deploy-it/queue", "directory containing queued ZIP responses")
	logPath := flag.String("log-file", "/tmp/dibra-deploy-it/requests.log", "request log path")
	flag.Parse()

	if err := os.MkdirAll(*queueDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	s := &server{queueDir: *queueDir, logPath: *logPath}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/gettasks", s.getTasks)
	mux.HandleFunc("/gettasks_outcome", s.recordOutcome)
	if err := http.ListenAndServe(*listen, mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (s *server) getTasks(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.queueDir)
	if err != nil {
		s.log(http.StatusInternalServerError, err.Error())
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		archivePath := filepath.Join(s.queueDir, entry.Name())
		archive, readErr := os.ReadFile(archivePath)
		if readErr != nil {
			s.log(http.StatusInternalServerError, readErr.Error())
			http.Error(writer, readErr.Error(), http.StatusInternalServerError)
			return
		}
		taskID := strings.TrimSuffix(entry.Name(), ".zip")
		writer.Header().Set("Content-Type", "application/zip")
		writer.Header().Set("X-Dibra-Task-ID", taskID)
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(archive)
		s.log(http.StatusOK, entry.Name())
		return
	}
	writer.WriteHeader(http.StatusNoContent)
	s.log(http.StatusNoContent, "empty")
}

func (s *server) recordOutcome(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var outcome taskOutcome
	if err := json.NewDecoder(request.Body).Decode(&outcome); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	if outcome.TaskID == "" || filepath.Base(outcome.TaskID) != outcome.TaskID {
		http.Error(writer, "invalid task ID", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	archivePath := filepath.Join(s.queueDir, outcome.TaskID+".zip")
	if err := os.Rename(archivePath, archivePath+".served"); err != nil {
		if _, statErr := os.Stat(archivePath + ".served"); statErr == nil {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		s.log(http.StatusNotFound, err.Error())
		http.Error(writer, err.Error(), http.StatusNotFound)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
	s.log(http.StatusNoContent, "outcome "+outcome.TaskID+" "+outcome.Status)
}

func (s *server) log(status int, detail string) {
	file, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, "%s %d %s\n", time.Now().UTC().Format(time.RFC3339Nano), status, detail)
}
