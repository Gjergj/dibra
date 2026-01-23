package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/gjergjiramku/goansible/internal/modules/apt"
	"github.com/gjergjiramku/goansible/internal/modules/apt_key"
	"github.com/gjergjiramku/goansible/internal/modules/apt_repository"
	"github.com/gjergjiramku/goansible/internal/modules/copy"
	"github.com/gjergjiramku/goansible/internal/modules/cron"
	"github.com/gjergjiramku/goansible/internal/modules/file"
	"github.com/gjergjiramku/goansible/internal/modules/stat"
	"github.com/gjergjiramku/goansible/internal/modules/uri"
)

type ModuleRequest struct {
	Module string          `json:"module"`
	Args   json.RawMessage `json:"args"`
}

func main() {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		writeError(fmt.Sprintf("failed to read stdin: %v", err))
		return
	}

	jsonStart := bytes.IndexByte(input, '{')
	if jsonStart == -1 {
		writeError("no JSON object found in input")
		return
	}
	input = input[jsonStart:]

	var modReq ModuleRequest
	if err := json.Unmarshal(input, &modReq); err != nil {
		writeError(fmt.Sprintf("failed to parse module request: %v", err))
		return
	}

	switch modReq.Module {
	case "apt":
		var req apt.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse apt request: %v", err))
			return
		}
		writeJSON(apt.Execute(req))

	case "apt_key":
		var req apt_key.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse apt_key request: %v", err))
			return
		}
		writeJSON(apt_key.Execute(req))

	case "apt_repository":
		var req apt_repository.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse apt_repository request: %v", err))
			return
		}
		writeJSON(apt_repository.Execute(req))

	case "file":
		var req file.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse file request: %v", err))
			return
		}
		writeJSON(file.Execute(req))

	case "copy":
		var req copy.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse copy request: %v", err))
			return
		}
		writeJSON(copy.Execute(req))

	case "stat":
		var req stat.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse stat request: %v", err))
			return
		}
		writeJSON(stat.Execute(req))

	case "uri":
		var req uri.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse uri request: %v", err))
			return
		}
		writeJSON(uri.Execute(req))

	case "cron":
		var req cron.Request
		if err := json.Unmarshal(modReq.Args, &req); err != nil {
			writeError(fmt.Sprintf("failed to parse cron request: %v", err))
			return
		}
		writeJSON(cron.Execute(req))

	default:
		writeError(fmt.Sprintf("unknown module: %s", modReq.Module))
	}
}

func writeError(msg string) {
	resp := map[string]interface{}{
		"failed": true,
		"msg":    msg,
	}
	writeJSON(resp)
}

func writeJSON(v interface{}) {
	data, _ := json.Marshal(v)
	fmt.Println(string(data))
}
