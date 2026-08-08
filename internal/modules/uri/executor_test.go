package uri

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteMultipartForm(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
			t.Errorf("unexpected content type: %q", r.Header.Get("Content-Type"))
		}

		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		if got := r.FormValue("username"); got != "testuser" {
			t.Errorf("username = %q, want testuser", got)
		}

		file, header, err := r.FormFile("document")
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read form file: %v", err)
		}
		if string(data) != "hello from multipart" {
			t.Errorf("file content = %q, want hello from multipart", data)
		}
		if header.Filename != "document.txt" {
			t.Errorf("file name = %q, want document.txt", header.Filename)
		}
		if header.Header.Get("Content-Type") != "text/plain" {
			t.Errorf("file content type = %q, want text/plain", header.Header.Get("Content-Type"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	resp := Execute(Request{
		URL:        server.URL,
		Method:     "POST",
		BodyFormat: "form-multipart",
		Body: map[string]interface{}{
			"username": "testuser",
			"document": map[string]interface{}{
				"content":   "hello from multipart",
				"filename":  "document.txt",
				"mime_type": "text/plain",
			},
		},
		ReturnContent: true,
	})

	if resp.Failed {
		t.Fatalf("multipart request failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Fatal("POST multipart request should report changed=true")
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Status, http.StatusOK)
	}
}

func TestExecuteMultipartFilePath(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "upload.txt")
	if err := os.WriteFile(filePath, []byte("file upload"), 0o600); err != nil {
		t.Fatalf("write upload file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		file, header, err := r.FormFile("upload")
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read form file: %v", err)
		}
		if string(data) != "file upload" {
			t.Errorf("file content = %q, want file upload", data)
		}
		if header.Filename != "upload.txt" {
			t.Errorf("file name = %q, want upload.txt", header.Filename)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	resp := Execute(Request{
		URL:        server.URL,
		Method:     "POST",
		BodyFormat: "form-multipart",
		Body: map[string]interface{}{
			"upload": map[string]interface{}{
				"filename": filePath,
			},
		},
		StatusCode: []int{http.StatusNoContent},
	})

	if resp.Failed {
		t.Fatalf("multipart file request failed: %s", resp.Msg)
	}
}

func TestExecuteMultipartBase64Field(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("multipart reader: %v", err)
		}
		part, err := reader.NextPart()
		if err != nil {
			t.Fatalf("first multipart part: %v", err)
		}
		if part.Header.Get("Content-Transfer-Encoding") != "base64" {
			t.Errorf("content transfer encoding = %q, want base64", part.Header.Get("Content-Transfer-Encoding"))
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read encoded part: %v", err)
		}
		if string(data) != "aGVsbG8=\r\n" {
			t.Errorf("encoded content = %q, want aGVsbG8=\\r\\n", data)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	resp := Execute(Request{
		URL:        server.URL,
		Method:     "POST",
		BodyFormat: "form-multipart",
		Body: map[string]interface{}{
			"payload": map[string]interface{}{
				"content":            "hello",
				"multipart_encoding": "base64",
			},
		},
		StatusCode: []int{http.StatusNoContent},
	})

	if resp.Failed {
		t.Fatalf("multipart base64 request failed: %s", resp.Msg)
	}
}

func TestExecuteRejectsInvalidMultipartBody(t *testing.T) {
	resp := Execute(Request{
		URL:        "http://127.0.0.1:1",
		BodyFormat: "form-multipart",
		Body:       "not a mapping",
	})

	if !resp.Failed {
		t.Fatal("expected invalid multipart body to fail")
	}
	if !strings.Contains(resp.Msg, "form-multipart body must be a mapping") {
		t.Fatalf("unexpected error: %s", resp.Msg)
	}
}

func TestExecuteRejectsDeprecatedFollowRedirectsValues(t *testing.T) {
	for _, test := range []struct {
		value       string
		replacement string
	}{
		{value: "no", replacement: "none"},
		{value: "yes", replacement: "all"},
	} {
		t.Run(test.value, func(t *testing.T) {
			resp := Execute(Request{
				URL:             "http://127.0.0.1:1",
				FollowRedirects: test.value,
			})

			if !resp.Failed {
				t.Fatal("expected deprecated redirect value to fail")
			}
			if !strings.Contains(resp.Msg, "deprecated and unsupported") || !strings.Contains(resp.Msg, test.replacement) {
				t.Fatalf("unexpected error: %s", resp.Msg)
			}
		})
	}
}
