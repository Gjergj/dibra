//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
)

// httpbinURL is the base URL for the httpbin service running in the test docker network
const httpbinURL = "http://httpbin:80"

func TestPlaybook_URIGet(t *testing.T) {
	playbook := playbookHeader + `
  - name: Make GET request
    uri:
      url: http://httpbin:80/get
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "200 OK") {
		t.Errorf("Expected 200 OK status, got: %s", output)
	}
}

func TestPlaybook_URIPost(t *testing.T) {
	playbook := playbookHeader + `
  - name: Make POST request with JSON body
    uri:
      url: http://httpbin:80/post
      method: POST
      body: '{"key": "value", "number": 42}'
      body_format: json
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for POST request")
	}
}

func TestPlaybook_URIStatusCode(t *testing.T) {
	playbook := playbookHeader + `
  - name: Accept 404 as success
    uri:
      url: http://httpbin:80/status/404
      status_code:
        - 404
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success with 404 accepted, got: %s", output)
	}
}

func TestPlaybook_URIStatusCodeFail(t *testing.T) {
	playbook := playbookHeader + `
  - name: Fail on unexpected status
    uri:
      url: http://httpbin:80/status/500
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED for 500 status")
	}
}

func TestPlaybook_URIHeaders(t *testing.T) {
	playbook := playbookHeader + `
  - name: Request with custom headers
    uri:
      url: http://httpbin:80/headers
      headers:
        X-Custom-Header: "test-value"
        Accept: "application/json"
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIDownload(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destFile := "/tmp/goansible-uri-download.txt"
	client.Run("rm -f " + destFile)

	playbook := playbookHeader + `
  - name: Download file via URI
    uri:
      url: http://httpbin:80/robots.txt
      dest: /tmp/goansible-uri-download.txt
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for download")
	}

	if !remoteFileExists(t, client, destFile) {
		t.Error("Downloaded file should exist")
	}

	client.Run("rm -f " + destFile)
}

func TestPlaybook_URICreates(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	markerFile := "/tmp/goansible-uri-creates-marker"
	client.Run("touch " + markerFile)
	defer client.Run("rm -f " + markerFile)

	playbook := playbookHeader + `
  - name: Skip request if file exists
    uri:
      url: http://httpbin:80/get
      creates: /tmp/goansible-uri-creates-marker
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "OK") {
		t.Error("Expected OK (skipped)")
	}
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes when creates file exists")
	}
}

func TestPlaybook_URITimeout(t *testing.T) {
	playbook := playbookHeader + `
  - name: Request with short timeout
    uri:
      url: http://httpbin:80/delay/5
      timeout: 2
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED for timeout")
	}
	if !strings.Contains(output, "timed out") {
		t.Error("Expected timeout error message")
	}
}

func TestPlaybook_URIFollowRedirects(t *testing.T) {
	playbook := playbookHeader + `
  - name: Follow redirect
    uri:
      url: http://httpbin:80/redirect/1
      follow_redirects: all
      status_code:
        - 200
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success after following redirect, got: %s", output)
	}
}

func TestPlaybook_URINoFollowRedirects(t *testing.T) {
	playbook := playbookHeader + `
  - name: Do not follow redirect
    uri:
      url: http://httpbin:80/redirect/1
      follow_redirects: none
      status_code:
        - 302
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success with 302, got: %s", output)
	}
}

func TestPlaybook_URIPut(t *testing.T) {
	playbook := playbookHeader + `
  - name: Make PUT request
    uri:
      url: http://httpbin:80/put
      method: PUT
      body: '{"update": "data"}'
      body_format: json
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for PUT request")
	}
}

func TestPlaybook_URIPatch(t *testing.T) {
	playbook := playbookHeader + `
  - name: Make PATCH request
    uri:
      url: http://httpbin:80/patch
      method: PATCH
      body: '{"partial": "update"}'
      body_format: json
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for PATCH request")
	}
}

func TestPlaybook_URIDelete(t *testing.T) {
	playbook := playbookHeader + `
  - name: Make DELETE request
    uri:
      url: http://httpbin:80/delete
      method: DELETE
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for DELETE request")
	}
}

func TestPlaybook_URIFormUrlencoded(t *testing.T) {
	playbook := playbookHeader + `
  - name: Make POST request with form data
    uri:
      url: http://httpbin:80/post
      method: POST
      body: "username=testuser&password=testpass"
      body_format: form-urlencoded
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for POST request")
	}
}

func TestPlaybook_URIBasicAuth(t *testing.T) {
	playbook := playbookHeader + `
  - name: Make request with basic auth
    uri:
      url: http://httpbin:80/basic-auth/testuser/testpass
      url_username: testuser
      url_password: testpass
      force_basic_auth: true
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success with basic auth, got: %s", output)
	}
	if !strings.Contains(output, "200 OK") {
		t.Error("Expected 200 OK for authenticated request")
	}
}

func TestPlaybook_URIBasicAuthFailed(t *testing.T) {
	playbook := playbookHeader + `
  - name: Make request with wrong credentials
    uri:
      url: http://httpbin:80/basic-auth/testuser/testpass
      url_username: wronguser
      url_password: wrongpass
      force_basic_auth: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED for wrong credentials")
	}
}

func TestPlaybook_URIMultipleStatusCodes(t *testing.T) {
	playbook := playbookHeader + `
  - name: Accept multiple status codes
    uri:
      url: http://httpbin:80/status/201
      status_code:
        - 200
        - 201
        - 202
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success with 201 in accepted codes, got: %s", output)
	}
}

func TestPlaybook_URIDownloadToDirectory(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-uri-download-dir"
	client.Run("rm -rf " + destDir)
	client.Run("mkdir -p " + destDir)

	playbook := playbookHeader + `
  - name: Download file to directory
    uri:
      url: http://httpbin:80/robots.txt
      dest: /tmp/goansible-uri-download-dir
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Should use filename from URL
	if !remoteFileExists(t, client, destDir+"/robots.txt") {
		t.Error("Downloaded file should exist with correct filename")
	}

	client.Run("rm -rf " + destDir)
}

func TestPlaybook_URIDownloadCreatesParentDirs(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destFile := "/tmp/goansible-uri-nested/subdir/download.txt"
	client.Run("rm -rf /tmp/goansible-uri-nested")

	playbook := playbookHeader + `
  - name: Download file to nested path
    uri:
      url: http://httpbin:80/robots.txt
      dest: /tmp/goansible-uri-nested/subdir/download.txt
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	if !remoteFileExists(t, client, destFile) {
		t.Error("Downloaded file should exist")
	}

	if !remoteDirExists(t, client, "/tmp/goansible-uri-nested/subdir") {
		t.Error("Parent directories should be created")
	}

	client.Run("rm -rf /tmp/goansible-uri-nested")
}

func TestPlaybook_URIGetNotChanged(t *testing.T) {
	playbook := playbookHeader + `
  - name: GET request should not be changed
    uri:
      url: http://httpbin:80/get
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if strings.Contains(output, "CHANGED") {
		t.Error("GET request without dest should not be CHANGED")
	}
}

func TestPlaybook_URIHeadRequest(t *testing.T) {
	playbook := playbookHeader + `
  - name: Make HEAD request
    uri:
      url: http://httpbin:80/get
      method: HEAD
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if strings.Contains(output, "CHANGED") {
		t.Error("HEAD request should not be CHANGED")
	}
}

func TestPlaybook_URISafeRedirectPost(t *testing.T) {
	// Safe redirect mode should not follow redirects for POST
	playbook := playbookHeader + `
  - name: POST with safe redirect mode
    uri:
      url: http://httpbin:80/redirect-to?url=http://httpbin:80/post&status_code=307
      method: POST
      body: '{"test": "data"}'
      body_format: json
      follow_redirects: safe
      status_code:
        - 307
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success with 307 (not following redirect for POST), got: %s", output)
	}
}

func TestPlaybook_URIMultipleRedirects(t *testing.T) {
	playbook := playbookHeader + `
  - name: Follow multiple redirects
    uri:
      url: http://httpbin:80/redirect/3
      follow_redirects: all
      status_code:
        - 200
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success after following multiple redirects, got: %s", output)
	}
}

func TestPlaybook_URIAbsoluteRedirect(t *testing.T) {
	playbook := playbookHeader + `
  - name: Follow absolute redirect
    uri:
      url: http://httpbin:80/absolute-redirect/1
      follow_redirects: all
      status_code:
        - 200
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success after following absolute redirect, got: %s", output)
	}
}

func TestPlaybook_URIUserAgent(t *testing.T) {
	playbook := playbookHeader + `
  - name: Request with custom User-Agent
    uri:
      url: http://httpbin:80/user-agent
      headers:
        User-Agent: "CustomAgent/1.0"
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIAcceptHeader(t *testing.T) {
	playbook := playbookHeader + `
  - name: Request with Accept header
    uri:
      url: http://httpbin:80/headers
      headers:
        Accept: "application/xml"
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIResponseHeaders(t *testing.T) {
	playbook := playbookHeader + `
  - name: Check response headers
    uri:
      url: http://httpbin:80/response-headers?X-Test-Header=test-value
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIGzip(t *testing.T) {
	playbook := playbookHeader + `
  - name: Request gzip endpoint
    uri:
      url: http://httpbin:80/gzip
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIDeflate(t *testing.T) {
	playbook := playbookHeader + `
  - name: Request deflate endpoint
    uri:
      url: http://httpbin:80/deflate
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIJsonResponse(t *testing.T) {
	playbook := playbookHeader + `
  - name: Get JSON response
    uri:
      url: http://httpbin:80/json
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIStatusCodes(t *testing.T) {
	// Test various status codes
	testCases := []struct {
		code int
	}{
		{200},
		{201},
		{204},
		{400},
		{401},
		{403},
		{404},
		{500},
		{502},
		{503},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Status%d", tc.code), func(t *testing.T) {
			playbook := playbookHeader + fmt.Sprintf(`
  - name: Test status code %d
    uri:
      url: http://httpbin:80/status/%d
      status_code:
        - %d
`, tc.code, tc.code, tc.code)
			output := runPlaybook(t, playbook)

			if strings.Contains(output, "FAILED") {
				t.Errorf("Expected success with status %d accepted, got: %s", tc.code, output)
			}
		})
	}
}

func TestPlaybook_URICreatesNonExistent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	markerFile := "/tmp/goansible-uri-creates-nonexistent"
	client.Run("rm -f " + markerFile)

	playbook := playbookHeader + `
  - name: Request when creates file does not exist
    uri:
      url: http://httpbin:80/get
      creates: /tmp/goansible-uri-creates-nonexistent
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	// Should execute since file doesn't exist
	if !strings.Contains(output, "200 OK") {
		t.Error("Expected request to execute when creates file doesn't exist")
	}
}

func TestPlaybook_URIBytes(t *testing.T) {
	playbook := playbookHeader + `
  - name: Request bytes endpoint
    uri:
      url: http://httpbin:80/bytes/100
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIImage(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destFile := "/tmp/goansible-uri-image.png"
	client.Run("rm -f " + destFile)

	playbook := playbookHeader + `
  - name: Download image
    uri:
      url: http://httpbin:80/image/png
      dest: /tmp/goansible-uri-image.png
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	if !remoteFileExists(t, client, destFile) {
		t.Error("Downloaded image should exist")
	}

	client.Run("rm -f " + destFile)
}

func TestPlaybook_URIDelay(t *testing.T) {
	playbook := playbookHeader + `
  - name: Request with delay
    uri:
      url: http://httpbin:80/delay/1
      timeout: 10
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIAnything(t *testing.T) {
	playbook := playbookHeader + `
  - name: Test anything endpoint
    uri:
      url: http://httpbin:80/anything
      method: POST
      body: '{"anything": "works"}'
      body_format: json
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for POST request")
	}
}

func TestPlaybook_URIUuid(t *testing.T) {
	playbook := playbookHeader + `
  - name: Get UUID
    uri:
      url: http://httpbin:80/uuid
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIIp(t *testing.T) {
	playbook := playbookHeader + `
  - name: Get IP address
    uri:
      url: http://httpbin:80/ip
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIQueryParams(t *testing.T) {
	playbook := playbookHeader + `
  - name: Request with query parameters
    uri:
      url: "http://httpbin:80/get?param1=value1&param2=value2"
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIPostRawBody(t *testing.T) {
	playbook := playbookHeader + `
  - name: POST with raw body
    uri:
      url: http://httpbin:80/post
      method: POST
      body: "This is raw text content"
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for POST request")
	}
}

func TestPlaybook_URIEmptyBody(t *testing.T) {
	playbook := playbookHeader + `
  - name: POST with empty body
    uri:
      url: http://httpbin:80/post
      method: POST
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for POST request")
	}
}

func TestPlaybook_URILargeResponse(t *testing.T) {
	playbook := playbookHeader + `
  - name: Request large response
    uri:
      url: http://httpbin:80/bytes/10000
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIContentTypeOverride(t *testing.T) {
	playbook := playbookHeader + `
  - name: POST with custom Content-Type
    uri:
      url: http://httpbin:80/post
      method: POST
      body: "<xml>data</xml>"
      headers:
        Content-Type: "application/xml"
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URICookies(t *testing.T) {
	playbook := playbookHeader + `
  - name: Set cookies
    uri:
      url: http://httpbin:80/cookies/set?name=value
      follow_redirects: all
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIBase64(t *testing.T) {
	playbook := playbookHeader + `
  - name: Decode base64
    uri:
      url: http://httpbin:80/base64/SGVsbG8gV29ybGQ=
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIDrip(t *testing.T) {
	playbook := playbookHeader + `
  - name: Request drip endpoint (streaming)
    uri:
      url: http://httpbin:80/drip?duration=1&numbytes=5&code=200
      timeout: 10
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIDownloadIdempotent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destFile := "/tmp/goansible-uri-idempotent.txt"
	client.Run("rm -f " + destFile)

	playbook := playbookHeader + `
  - name: Download file
    uri:
      url: http://httpbin:80/robots.txt
      dest: /tmp/goansible-uri-idempotent.txt
`
	// First run
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success on first run, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED on first download")
	}

	// Second run - still changed because uri module doesn't check content
	// (this is expected behavior matching Ansible)
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success on second run, got: %s", output)
	}

	client.Run("rm -f " + destFile)
}
