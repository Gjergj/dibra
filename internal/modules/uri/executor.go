package uri

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func Execute(req Request) Response {
	if req.URL == "" {
		return Response{Failed: true, Msg: "url is required"}
	}

	if req.Creates != "" {
		if _, err := os.Stat(req.Creates); err == nil {
			return Response{
				Changed: false,
				Msg:     fmt.Sprintf("skipped, creates=%s exists", req.Creates),
			}
		}
	}

	if req.Method == "" {
		req.Method = "GET"
	}
	if req.BodyFormat == "" {
		req.BodyFormat = "raw"
	}
	if req.Timeout == 0 {
		req.Timeout = 30
	}
	if len(req.StatusCode) == 0 {
		req.StatusCode = []int{200}
	}
	if req.FollowRedirects == "" {
		req.FollowRedirects = "safe"
	}

	validateCerts := true
	if req.ValidateCerts != nil {
		validateCerts = *req.ValidateCerts
	}

	var bodyReader io.Reader
	var contentType string

	if req.Body != "" {
		switch req.BodyFormat {
		case "json":
			contentType = "application/json"
			bodyReader = strings.NewReader(req.Body)
		case "form-urlencoded":
			contentType = "application/x-www-form-urlencoded"
			bodyReader = strings.NewReader(req.Body)
		default:
			bodyReader = strings.NewReader(req.Body)
		}
	}

	httpReq, err := http.NewRequest(strings.ToUpper(req.Method), req.URL, bodyReader)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create request: %v", err)}
	}

	httpReq.Header.Set("User-Agent", "dibra-uri/1.0")

	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}

	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	if req.URLUsername != "" && req.ForceBasicAuth {
		httpReq.SetBasicAuth(req.URLUsername, req.URLPassword)
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: !validateCerts,
		},
	}

	client := &http.Client{
		Timeout:   time.Duration(req.Timeout) * time.Second,
		Transport: transport,
	}

	switch req.FollowRedirects {
	case "none", "no":
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	case "safe":
		client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
			if r.Method != "GET" && r.Method != "HEAD" {
				return http.ErrUseLastResponse
			}
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil
		}
	}

	startTime := time.Now()
	resp, err := client.Do(httpReq)
	elapsed := int(time.Since(startTime).Seconds())

	if err != nil {
		if urlErr, ok := err.(*url.Error); ok && urlErr.Timeout() {
			return Response{
				Failed:  true,
				Msg:     fmt.Sprintf("request timed out after %d seconds", req.Timeout),
				Elapsed: elapsed,
			}
		}
		return Response{Failed: true, Msg: fmt.Sprintf("request failed: %v", err), Elapsed: elapsed}
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to read response body: %v", err), Status: resp.StatusCode}
	}

	result := Response{
		Status:        resp.StatusCode,
		URL:           resp.Request.URL.String(),
		ContentLength: resp.ContentLength,
		ContentType:   resp.Header.Get("Content-Type"),
		Redirected:    resp.Request.URL.String() != req.URL,
		Elapsed:       elapsed,
		Msg:           resp.Status,
		Headers:       make(map[string]string),
	}

	for k := range resp.Header {
		result.Headers[strings.ToLower(k)] = resp.Header.Get(k)
	}

	statusOK := false
	for _, code := range req.StatusCode {
		if resp.StatusCode == code {
			statusOK = true
			break
		}
	}

	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		var jsonData interface{}
		if err := json.Unmarshal(bodyBytes, &jsonData); err == nil {
			result.JSON = jsonData
		}
	}

	if req.ReturnContent || !statusOK {
		result.Content = string(bodyBytes)
	}

	if !statusOK {
		result.Failed = true
		result.Msg = fmt.Sprintf("Status code was %d and not %v: %s", resp.StatusCode, req.StatusCode, resp.Status)
		return result
	}

	if req.Dest != "" {
		destPath := req.Dest

		info, err := os.Stat(req.Dest)
		if err == nil && info.IsDir() {
			filename := filepath.Base(resp.Request.URL.Path)
			if filename == "" || filename == "/" {
				filename = "index.html"
			}
			destPath = filepath.Join(req.Dest, filename)
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create destination directory: %v", err), Status: resp.StatusCode}
		}

		if err := os.WriteFile(destPath, bodyBytes, 0644); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to write to dest: %v", err), Status: resp.StatusCode}
		}

		result.Path = destPath
		result.Changed = true
	}

	if req.Dest == "" && (req.Method == "POST" || req.Method == "PUT" || req.Method == "PATCH" || req.Method == "DELETE") {
		result.Changed = true
	}

	return result
}
