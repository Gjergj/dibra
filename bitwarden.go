package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

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

func unlockBitwarden(password string) (string, error) {

	//first run bw sync
	cmd := exec.Command("bw", "sync")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to sync bitwarden: %s", err)
	}

	if password == "" {
		fmt.Print("Enter Bitwarden master password: ")
		p, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			return "", fmt.Errorf("failed to read password: %s", err)
		}
		fmt.Println()
		password = string(p)
	}

	cmd = exec.Command("bw", "unlock", "--response")
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
	return response.Data.Raw, nil
}

type bitwardenItem struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		PasswordHistory []struct {
			LastUsedDate time.Time `json:"lastUsedDate"`
			Password     string    `json:"password"`
		} `json:"passwordHistory"`
		RevisionDate   time.Time `json:"revisionDate"`
		CreationDate   time.Time `json:"creationDate"`
		DeletedDate    any       `json:"deletedDate"`
		Object         string    `json:"object"`
		ID             string    `json:"id"`
		OrganizationID any       `json:"organizationId"`
		FolderID       any       `json:"folderId"`
		Type           int       `json:"type"`
		Reprompt       int       `json:"reprompt"`
		Name           string    `json:"name"`
		Notes          string    `json:"notes"`
		Favorite       bool      `json:"favorite"`
		Fields         []struct {
			Name     string `json:"name"`
			Value    string `json:"value"`
			Type     int    `json:"type"`
			LinkedID any    `json:"linkedId"`
		} `json:"fields"`
		Login struct {
			Fido2Credentials     []any     `json:"fido2Credentials"`
			Uris                 []any     `json:"uris"`
			Username             string    `json:"username"`
			Password             string    `json:"password"`
			Totp                 any       `json:"totp"`
			PasswordRevisionDate time.Time `json:"passwordRevisionDate"`
		} `json:"login"`
		CollectionIds []any `json:"collectionIds"`
	} `json:"data"`
}

func getSecrets(session string, secretsList map[string]string) (map[string]string, error) {
	collections := make(map[string]interface{})

	secrets := make(map[string]string)
	for _, value := range secretsList {
		parts := strings.Split(value, "/")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid secret format: %s (expected collection/field)", value)
		}
		collection := parts[0]
		collections[collection] = interface{}(nil)
	}

	for collection := range collections {
		item, err := getBitwardenItem(session, collection)
		if err != nil {
			return nil, err
		}
		if collection == item.Data.Name {
			found := false
			for k, secret := range secretsList {
				parts := strings.Split(secret, "/")
				if len(parts) != 2 {
					return nil, fmt.Errorf("invalid secret format: %s (expected collection/field)", secret)
				}
				field := parts[1]
				for _, f := range item.Data.Fields {
					if f.Name == field {
						secrets[k] = f.Value
						found = true
					}
				}
				if field == "password" {
					secrets[k] = item.Data.Login.Password
					found = true
				} else if field == "username" {
					secrets[k] = item.Data.Login.Username
					found = true
				}
				if !found {
					return nil, fmt.Errorf("field %s not found in collection %s", field, collection)
				}
			}

		}
	}
	return secrets, nil
}

func getBitwardenItem(session string, item string) (bitwardenItem, error) {
	cmd := exec.Command("bw", "get", "item", item, "--session", session, "--response")
	cmd.Stderr = os.Stderr

	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb

	if err := cmd.Run(); err != nil {
		return bitwardenItem{}, fmt.Errorf("command failed: %s %s", err.Error(), outb.String())
	}

	var bwi bitwardenItem
	err := json.Unmarshal(outb.Bytes(), &bwi)
	if err != nil {
		return bitwardenItem{}, fmt.Errorf("failed to unmarshal response: %s", err)
	}

	return bwi, nil
}
