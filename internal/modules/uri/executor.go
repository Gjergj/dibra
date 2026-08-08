package uri

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func Execute(req Request) Response {
	if req.URL == "" {
		return Response{Failed: true, Msg: "url is required"}
	}

	if req.FollowRedirects != "" {
		req.FollowRedirects = strings.ToLower(req.FollowRedirects)
		if req.FollowRedirects == "no" || req.FollowRedirects == "yes" {
			replacement := "none"
			if req.FollowRedirects == "yes" {
				replacement = "all"
			}
			return Response{Failed: true, Msg: fmt.Sprintf("follow_redirects=%q is deprecated and unsupported; use %q instead", req.FollowRedirects, replacement)}
		}
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
	if req.FollowRedirects != "all" && req.FollowRedirects != "none" && req.FollowRedirects != "safe" && req.FollowRedirects != "urllib2" {
		return Response{Failed: true, Msg: fmt.Sprintf("invalid follow_redirects value %q; expected all, none, safe, or urllib2", req.FollowRedirects)}
	}

	validateCerts := true
	if req.ValidateCerts != nil {
		validateCerts = *req.ValidateCerts
	}

	bodyReader, contentType, forceContentType, err := prepareBody(req.Body, req.BodyFormat)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to prepare request body: %v", err)}
	}

	httpReq, err := http.NewRequest(strings.ToUpper(req.Method), req.URL, bodyReader)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create request: %v", err)}
	}

	httpReq.Header.Set("User-Agent", "dibra-uri/1.0")

	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	if contentType != "" && (forceContentType || httpReq.Header.Get("Content-Type") == "") {
		httpReq.Header.Set("Content-Type", contentType)
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
	case "none":
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
	case "all", "urllib2":
		// The default http.Client behavior follows redirects.
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

func prepareBody(body interface{}, bodyFormat string) (io.Reader, string, bool, error) {
	bodyFormat = strings.ToLower(bodyFormat)
	switch bodyFormat {
	case "raw", "json", "form-urlencoded", "form-multipart":
	default:
		return nil, "", false, fmt.Errorf("unsupported body_format %q; expected raw, json, form-urlencoded, or form-multipart", bodyFormat)
	}

	if body == nil {
		return nil, "", false, nil
	}

	switch bodyFormat {
	case "raw":
		data, err := rawBodyBytes(body)
		if err != nil {
			return nil, "", false, err
		}
		return bytes.NewReader(data), "", false, nil
	case "json":
		if value, ok := body.(string); ok {
			return strings.NewReader(value), "application/json", false, nil
		}
		data, err := json.Marshal(body)
		if err != nil {
			return nil, "", false, fmt.Errorf("failed to encode JSON body: %w", err)
		}
		return bytes.NewReader(data), "application/json", false, nil
	case "form-urlencoded":
		data, err := formURLEncodedBody(body)
		if err != nil {
			return nil, "", false, err
		}
		return bytes.NewReader(data), "application/x-www-form-urlencoded", false, nil
	case "form-multipart":
		reader, contentType, err := multipartBody(body)
		if err != nil {
			return nil, "", false, err
		}
		return reader, contentType, true, nil
	}
	return nil, "", false, fmt.Errorf("unsupported body_format %q", bodyFormat)
}

func rawBodyBytes(body interface{}) ([]byte, error) {
	switch value := body.(type) {
	case string:
		return []byte(value), nil
	case []byte:
		return value, nil
	default:
		return nil, fmt.Errorf("raw body must be a string or byte array, got %T", body)
	}
}

func formURLEncodedBody(body interface{}) ([]byte, error) {
	if value, ok := body.(string); ok {
		return []byte(value), nil
	}

	values := url.Values{}
	fields, ok := body.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("form-urlencoded body must be a string or mapping, got %T", body)
	}

	for key, value := range fields {
		items, err := formValues(value)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", key, err)
		}
		for _, item := range items {
			values.Add(key, item)
		}
	}

	return []byte(values.Encode()), nil
}

func formValues(value interface{}) ([]string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		return []string{typed}, nil
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return []string{fmt.Sprint(typed)}, nil
	case []interface{}:
		var result []string
		for _, item := range typed {
			items, err := formValues(item)
			if err != nil {
				return nil, err
			}
			result = append(result, items...)
		}
		return result, nil
	case []string:
		return typed, nil
	default:
		return nil, fmt.Errorf("expected a scalar or list, got %T", value)
	}
}

func multipartBody(body interface{}) (io.Reader, string, error) {
	fields, ok := body.(map[string]interface{})
	if !ok {
		return nil, "", fmt.Errorf("form-multipart body must be a mapping, got %T", body)
	}

	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if err := writeMultipartField(writer, key, fields[key]); err != nil {
			_ = writer.Close()
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart body: %w", err)
	}

	return bytes.NewReader(buffer.Bytes()), writer.FormDataContentType(), nil
}

func writeMultipartField(writer *multipart.Writer, field string, value interface{}) error {
	contentType := "text/plain"
	filename := ""
	encoding := ""
	var content []byte

	switch typed := value.(type) {
	case string:
		content = []byte(typed)
	case map[string]interface{}:
		var ok bool
		if rawFilename, exists := typed["filename"]; exists {
			filename, ok = rawFilename.(string)
			if !ok {
				return fmt.Errorf("multipart field %q filename must be a string", field)
			}
		}

		rawContent, hasContent := typed["content"]
		contentString := ""
		if hasContent && rawContent != nil {
			var contentOK bool
			contentString, contentOK = rawContent.(string)
			if !contentOK {
				return fmt.Errorf("multipart field %q content must be a string", field)
			}
		}

		if filename == "" && (!hasContent || rawContent == nil) {
			return fmt.Errorf("multipart field %q requires filename or content", field)
		}
		if filename != "" && contentString == "" {
			fileData, err := os.ReadFile(filename)
			if err != nil {
				return fmt.Errorf("multipart field %q: failed to read %q: %w", field, filename, err)
			}
			content = fileData
			filename = filepath.Base(filename)
		} else {
			content = []byte(contentString)
		}

		if rawMime, exists := typed["mime_type"]; exists && rawMime != nil {
			var mimeOK bool
			contentType, mimeOK = rawMime.(string)
			if !mimeOK {
				return fmt.Errorf("multipart field %q mime_type must be a string", field)
			}
		} else if filename != "" {
			if guessed := mime.TypeByExtension(filepath.Ext(filename)); guessed != "" {
				contentType = guessed
			} else {
				contentType = "application/octet-stream"
			}
		} else {
			contentType = "application/octet-stream"
		}

		if rawEncoding, exists := typed["multipart_encoding"]; exists && rawEncoding != nil {
			var encodingOK bool
			encoding, encodingOK = rawEncoding.(string)
			if !encodingOK {
				return fmt.Errorf("multipart field %q multipart_encoding must be a string", field)
			}
			if encoding != "base64" && encoding != "7or8bit" {
				return fmt.Errorf("multipart field %q multipart_encoding must be base64 or 7or8bit", field)
			}
		}
	default:
		return fmt.Errorf("multipart field %q must be a string or mapping, got %T", field, value)
	}

	header := textproto.MIMEHeader{}
	disposition := fmt.Sprintf(`form-data; name="%s"`, escapeMultipartHeaderValue(field))
	if filename != "" {
		disposition += fmt.Sprintf(`; filename="%s"`, escapeMultipartHeaderValue(filename))
	}
	header.Set("Content-Disposition", disposition)
	header.Set("Content-Type", contentType)

	if encoding == "base64" {
		header.Set("Content-Transfer-Encoding", "base64")
		content = base64Multipart(content)
	} else if encoding == "7or8bit" {
		header.Set("Content-Transfer-Encoding", "7bit")
	}

	part, err := writer.CreatePart(header)
	if err != nil {
		return fmt.Errorf("failed to create multipart field %q: %w", field, err)
	}
	if _, err := part.Write(content); err != nil {
		return fmt.Errorf("failed to write multipart field %q: %w", field, err)
	}
	return nil
}

func escapeMultipartHeaderValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func base64Multipart(content []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(content)
	var result bytes.Buffer
	for len(encoded) > 76 {
		result.WriteString(encoded[:76])
		result.WriteString("\r\n")
		encoded = encoded[76:]
	}
	result.WriteString(encoded)
	result.WriteString("\r\n")
	return result.Bytes()
}
