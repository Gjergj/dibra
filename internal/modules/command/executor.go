package command

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func Execute(req Request) Response {
	resp := Response{
		Changed:     false,
		Cmd:         []string{},
		Stdout:      "",
		Stderr:      "",
		StdoutLines: []string{},
		StderrLines: []string{},
		RC:          0,
	}

	var args []string
	if len(req.Argv) > 0 {
		args = req.Argv
	} else if req.Cmd != "" {
		args = splitCommand(req.Cmd)
	} else {
		resp.Failed = true
		resp.Msg = "one of cmd or argv is required"
		return resp
	}

	if len(args) == 0 {
		resp.Failed = true
		resp.Msg = "no command specified"
		return resp
	}

	resp.Cmd = args

	if req.Creates != "" {
		matches, _ := filepath.Glob(req.Creates)
		if len(matches) > 0 {
			resp.Msg = "skipped, since " + req.Creates + " exists"
			resp.Stdout = "skipped, since " + req.Creates + " exists"
			resp.StdoutLines = []string{resp.Stdout}
			return resp
		}
	}

	if req.Removes != "" {
		matches, _ := filepath.Glob(req.Removes)
		if len(matches) == 0 {
			resp.Msg = "skipped, since " + req.Removes + " does not exist"
			resp.Stdout = "skipped, since " + req.Removes + " does not exist"
			resp.StdoutLines = []string{resp.Stdout}
			return resp
		}
	}

	resp.Changed = true

	cmd := exec.Command(args[0], args[1:]...)

	if req.Chdir != "" {
		if _, err := os.Stat(req.Chdir); os.IsNotExist(err) {
			resp.Failed = true
			resp.Msg = "Unable to change directory before execution: directory does not exist: " + req.Chdir
			return resp
		}
		cmd.Dir = req.Chdir
	}

	if req.Stdin != "" {
		stdin := req.Stdin
		stdinAddNewline := true
		if req.StdinAddNewline != nil {
			stdinAddNewline = *req.StdinAddNewline
		}
		if stdinAddNewline && !strings.HasSuffix(stdin, "\n") {
			stdin += "\n"
		}
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	resp.Start = startTime.Format("2006-01-02 15:04:05.000000")

	err := cmd.Run()

	endTime := time.Now()
	resp.End = endTime.Format("2006-01-02 15:04:05.000000")
	resp.Delta = formatDelta(endTime.Sub(startTime))

	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	stripEmptyEnds := true
	if req.StripEmptyEnds != nil {
		stripEmptyEnds = *req.StripEmptyEnds
	}
	if stripEmptyEnds {
		stdoutStr = strings.TrimRight(stdoutStr, "\r\n")
		stderrStr = strings.TrimRight(stderrStr, "\r\n")
	}

	resp.Stdout = stdoutStr
	resp.Stderr = stderrStr
	resp.StdoutLines = splitLines(stdoutStr)
	resp.StderrLines = splitLines(stderrStr)

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			resp.RC = exitError.ExitCode()
		} else {
			resp.RC = 1
		}
		resp.Failed = true
		resp.Msg = "non-zero return code"
		return resp
	}

	resp.RC = 0
	return resp
}

func splitCommand(cmd string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range cmd {
		switch {
		case r == '"' || r == '\'':
			if !inQuote {
				inQuote = true
				quoteChar = r
			} else if r == quoteChar {
				inQuote = false
				quoteChar = 0
			} else {
				current.WriteRune(r)
			}
		case r == ' ' || r == '\t':
			if inQuote {
				current.WriteRune(r)
			} else if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}

func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func formatDelta(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	microseconds := (d.Nanoseconds() / 1000) % 1000000
	return strings.TrimSpace(strings.ReplaceAll(
		formatTimeDelta(hours, minutes, seconds, microseconds),
		"  ", " "))
}

func formatTimeDelta(hours, minutes, seconds int, microseconds int64) string {
	return strings.TrimSpace(strings.ReplaceAll(
		formatDurationStr(hours, minutes, seconds, microseconds),
		"  ", " "))
}

func formatDurationStr(hours, minutes, seconds int, microseconds int64) string {
	return strings.Join([]string{
		padInt(hours, 1),
		":",
		padInt(minutes, 2),
		":",
		padInt(seconds, 2),
		".",
		padInt64(microseconds, 6),
	}, "")
}

func padInt(n int, width int) string {
	s := strings.Repeat("0", width) + string(rune('0'+n%10))
	for n >= 10 {
		n /= 10
		s = string(rune('0'+n%10)) + s[1:]
	}
	result := make([]byte, 0, width)
	for i := len(s) - width; i < len(s); i++ {
		if i >= 0 {
			result = append(result, s[i])
		} else {
			result = append(result, '0')
		}
	}
	return string(result)
}

func padInt64(n int64, width int) string {
	s := ""
	for i := 0; i < width; i++ {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
