//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_URIGet(t *testing.T) {
	playbook := playbookHeader + `
  - name: Make GET request
    uri:
      url: https://httpbin.org/get
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
      url: https://httpbin.org/post
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
      url: https://httpbin.org/status/404
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
      url: https://httpbin.org/status/500
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
      url: https://httpbin.org/headers
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
      url: https://httpbin.org/robots.txt
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
      url: https://httpbin.org/get
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
      url: https://httpbin.org/delay/5
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
      url: https://httpbin.org/redirect/1
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
      url: https://httpbin.org/redirect/1
      follow_redirects: none
      status_code:
        - 302
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success with 302, got: %s", output)
	}
}
