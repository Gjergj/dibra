package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/term"
)

type BitwardenResponse struct {
	Success bool `json:"success"`
	Data    Data `json:"data"`
}

type Data struct {
	Raw     string `json:"raw"`
	Object  string `json:"object"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

func unlockBitwarden() (string, error) {
	fmt.Print("Enter Bitwarden master password: ")
	password, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", fmt.Errorf("failed to read password: %s", err)
	}
	fmt.Println()

	cmd := exec.Command("bw", "unlock", "--response")
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdin pipe: %s", err)
	}
	defer stdin.Close()

	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start command: %s", err)
	}

	_, err = stdin.Write([]byte(string(password) + "\n"))
	if err != nil {
		return "", fmt.Errorf("failed to write password: %s", err)
	}
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("command failed: %s %s", err.Error(), outb.String())
	}

	var response BitwardenResponse
	err = json.Unmarshal(outb.Bytes(), &response)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %s", err.Error())
	}

	if !response.Success {
		return "", fmt.Errorf("failed to unlock Bitwarden: %s", response.Data.Message)
	}

	session := strings.TrimSpace(response.Data.Raw)
	return session, nil
}

func getBitwardenItem(session string, item string) (string, error) {
	cmd := exec.Command("bw", "get", item, "--session", session, "--response")
	cmd.Stderr = os.Stderr

	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("command failed: %s %s", err.Error(), outb.String())
	}

	var response struct {
		PasswordHistory []interface{} `json:"passwordHistory"`
		RevisionDate    string        `json:"revisionDate"`
		CreationDate    string        `json:"creationDate"`
		DeletedDate     interface{}   `json:"deletedDate"`
		Object          string        `json:"object"`
		ID              string        `json:"id"`
		OrganizationID  interface{}   `json:"organizationId"`
		FolderID        interface{}   `json:"folderId"`
		Type            int           `json:"type"`
		Reprompt        int           `json:"reprompt"`
		Name            string        `json:"name"`
		Notes           interface{}   `json:"notes"`
		Favorite        bool          `json:"favorite"`
		Fields          []struct {
			Name     string      `json:"name"`
			Value    string      `json:"value"`
			Type     int         `json:"type"` // 0 plain text ; 2 hidden
			LinkedID interface{} `json:"linkedId"`
		} `json:"fields"`
		Login struct {
			Fido2Credentials     []interface{} `json:"fido2Credentials"`
			URIs                 []interface{} `json:"uris"`
			Username             string        `json:"username"`
			Password             string        `json:"password"`
			TOTP                 interface{}   `json:"totp"`
			PasswordRevisionDate interface{}   `json:"passwordRevisionDate"`
		} `json:"login"`
		CollectionIds []interface{} `json:"collectionIds"`
	}

	err := json.Unmarshal(outb.Bytes(), &response)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %s", err)
	}

	if !response.Success {
		return "", fmt.Errorf("failed to get item")
	}

	return outb.String(), nil
}
