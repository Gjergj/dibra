package apt_key

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	defaultKeyring = "/etc/apt/trusted.gpg.d"
	gpgCmd         = "/usr/bin/gpg"
)

func Execute(req Request) Response {
	if req.State == "" {
		req.State = "present"
	}

	switch req.State {
	case "present":
		return addKey(req)
	case "absent":
		return removeKey(req)
	default:
		return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", req.State)}
	}
}

func addKey(req Request) Response {
	var keyData []byte
	var err error
	var keyID string

	switch {
	case req.URL != "":
		keyData, err = downloadKey(req.URL)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to download key: %v", err)}
		}
	case req.Data != "":
		keyData = []byte(req.Data)
	case req.File != "":
		keyData, err = os.ReadFile(req.File)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to read key file: %v", err)}
		}
	default:
		return Response{Failed: true, Msg: "one of url, data, or file is required"}
	}

	keyID, err = extractKeyID(keyData)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to extract key ID: %v", err)}
	}

	keyring := req.Keyring
	if keyring == "" {
		keyring = filepath.Join(defaultKeyring, fmt.Sprintf("%s.gpg", keyID))
	}

	if keyExists(keyring, keyID) {
		return Response{Changed: false, Msg: "key already present", KeyID: keyID}
	}

	dearmored, err := dearmorKey(keyData)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to dearmor key: %v", err)}
	}

	if err := os.MkdirAll(filepath.Dir(keyring), 0755); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create keyring directory: %v", err)}
	}

	if err := os.WriteFile(keyring, dearmored, 0644); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to write keyring: %v", err)}
	}

	return Response{Changed: true, KeyID: keyID, Msg: fmt.Sprintf("key added to %s", keyring)}
}

func removeKey(req Request) Response {
	if req.ID == "" && req.Keyring == "" {
		return Response{Failed: true, Msg: "id or keyring is required for state=absent"}
	}

	keyring := req.Keyring
	if keyring == "" {
		keyring = filepath.Join(defaultKeyring, fmt.Sprintf("%s.gpg", req.ID))
	}

	if _, err := os.Stat(keyring); os.IsNotExist(err) {
		return Response{Changed: false, Msg: "key not present"}
	}

	if err := os.Remove(keyring); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to remove keyring: %v", err)}
	}

	return Response{Changed: true, Msg: fmt.Sprintf("key removed from %s", keyring)}
}

func downloadKey(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func extractKeyID(keyData []byte) (string, error) {
	cmd := exec.Command(gpgCmd, "--with-colons", "--show-keys")
	cmd.Stdin = bytes.NewReader(keyData)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gpg failed: %v", err)
	}

	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) >= 5 && (fields[0] == "pub" || fields[0] == "sec") {
			keyID := fields[4]
			if len(keyID) >= 8 {
				return keyID[len(keyID)-8:], nil
			}
		}
	}

	return "", fmt.Errorf("could not find key ID in gpg output")
}

func dearmorKey(keyData []byte) ([]byte, error) {
	if !bytes.Contains(keyData, []byte("-----BEGIN PGP")) {
		return keyData, nil
	}

	cmd := exec.Command(gpgCmd, "--dearmor")
	cmd.Stdin = bytes.NewReader(keyData)

	return cmd.Output()
}

func keyExists(keyring, keyID string) bool {
	if _, err := os.Stat(keyring); os.IsNotExist(err) {
		return false
	}

	cmd := exec.Command(gpgCmd, "--no-default-keyring", "--keyring", keyring, "--with-colons", "--list-keys")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	return strings.Contains(string(output), keyID)
}
