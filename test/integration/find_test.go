//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type FindFileInfo struct {
	Path     string  `json:"path"`
	Mode     string  `json:"mode"`
	IsDir    bool    `json:"isdir"`
	IsReg    bool    `json:"isreg"`
	IsLnk    bool    `json:"islnk"`
	UID      int     `json:"uid"`
	GID      int     `json:"gid"`
	Size     int64   `json:"size"`
	Atime    float64 `json:"atime"`
	Mtime    float64 `json:"mtime"`
	Ctime    float64 `json:"ctime"`
	GrName   string  `json:"gr_name"`
	PwName   string  `json:"pw_name"`
	Checksum string  `json:"checksum,omitempty"`
}

type FindResponse struct {
	Changed      bool              `json:"changed"`
	Failed       bool              `json:"failed,omitempty"`
	Msg          string            `json:"msg"`
	Files        []FindFileInfo    `json:"files"`
	Matched      int               `json:"matched"`
	Examined     int               `json:"examined"`
	SkippedPaths map[string]string `json:"skipped_paths"`
}

func getFindResult(t *testing.T, client interface {
	Run(string) (string, string, error)
}, args string) FindResponse {
	t.Helper()
	cmd := `echo '{"module":"find","args":` + args + `}' | /tmp/.dibra-agent`
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Fatalf("Agent execution failed: %v, stderr: %s", err, stderr)
	}
	var resp FindResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v, output: %s", err, stdout)
	}
	return resp
}

func findHasFile(files []FindFileInfo, name string) bool {
	for _, f := range files {
		if strings.HasSuffix(f.Path, "/"+name) || f.Path == name {
			return true
		}
	}
	return false
}

func TestPlaybook_Find(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Ensure agent is uploaded
	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	testDir := "/tmp/dibra-find-test"
	remoteExec(t, client, "rm -rf "+testDir)
	remoteExec(t, client, "mkdir -p "+testDir)
	defer remoteExec(t, client, "rm -rf "+testDir)

	// Create directory structure: a/b/c/d and e/f/g/h
	remoteExec(t, client, "mkdir -p "+testDir+"/a/b/c/d")
	remoteExec(t, client, "mkdir -p "+testDir+"/e/f/g/h")

	// Create files in various directories
	remoteExec(t, client, "echo -n 'data' > "+testDir+"/a/1.txt")
	remoteExec(t, client, "echo -n 'data' > "+testDir+"/a/b/2.jpg")
	remoteExec(t, client, "echo -n 'data' > "+testDir+"/a/b/c/3")
	remoteExec(t, client, "echo -n 'data' > "+testDir+"/a/b/c/d/4.xml")
	remoteExec(t, client, "echo -n 'data' > "+testDir+"/e/5.json")
	remoteExec(t, client, "echo -n 'data' > "+testDir+"/e/f/6.swp")
	remoteExec(t, client, "echo -n 'data' > "+testDir+"/e/f/g/7.img")
	remoteExec(t, client, "echo -n 'data' > "+testDir+"/e/f/g/h/8.ogg")

	t.Run("find directories recursively", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`"],"file_type":"directory","recurse":true}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if resp.Matched != 8 {
			t.Errorf("Expected 8 matched directories, got: %d", resp.Matched)
		}
	})

	t.Run("find files by glob pattern", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`"],"file_type":"file","patterns":["*.xml","*.img"],"recurse":true}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if resp.Matched != 2 {
			t.Errorf("Expected 2 matched files, got: %d", resp.Matched)
		}
	})

	t.Run("find single xml file", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`"],"patterns":["*.xml"],"recurse":true}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if resp.Matched != 1 {
			t.Errorf("Expected 1 matched file, got: %d", resp.Matched)
		}
	})

	t.Run("find with empty excludes", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`"],"patterns":["*.xml"],"excludes":[],"recurse":true}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if resp.Matched != 1 {
			t.Errorf("Expected 1 matched file, got: %d", resp.Matched)
		}
	})

	t.Run("find all with file_type any", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`"],"recurse":true,"file_type":"any"}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		// 8 dirs + 8 files = 16
		if resp.Matched != 16 {
			t.Errorf("Expected 16 matched, got: %d", resp.Matched)
		}
	})

	t.Run("no recurse finds only top level", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`"],"file_type":"any"}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		// Top level: a/ and e/ = 2 items
		if resp.Matched != 2 {
			t.Errorf("Expected 2 matched (top level dirs only), got: %d", resp.Matched)
		}
	})

	t.Run("exclude with regex", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`"],"recurse":true,"use_regex":true,"excludes":[".*\\.ogg"]}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if findHasFile(resp.Files, "8.ogg") {
			t.Errorf("Should not find ogg file")
		}
	})

	t.Run("patterns with regex", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`"],"recurse":true,"use_regex":true,"patterns":[".*\\.ogg"]}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if !findHasFile(resp.Files, "8.ogg") {
			t.Errorf("Should find ogg file")
		}
		if resp.Matched != 1 {
			t.Errorf("Expected 1 match, got: %d", resp.Matched)
		}
	})

	t.Run("find with depth limit", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Find with depth 2
    find:
      paths:
        - ` + testDir + `
      recurse: true
      file_type: any
      depth: 2
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success, got: %s", output)
		}
	})

	t.Run("find files with depth limit", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`"],"depth":2,"recurse":true}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
	})

	// Age/Size tests
	t.Run("age and size filtering", func(t *testing.T) {
		ageDir := testDir + "/astest"
		remoteExec(t, client, "mkdir -p "+ageDir)

		// Create old file
		remoteExec(t, client, "touch "+ageDir+"/old.txt")
		remoteExec(t, client, "touch -t 202001011200 "+ageDir+"/old.txt")

		// Create new file
		remoteExec(t, client, "touch "+ageDir+"/new.txt")

		// Create hidden file
		remoteExec(t, client, "touch "+ageDir+"/.hidden.txt")

		t.Run("find files older than 1 week", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+ageDir+`"],"age":"1w","hidden":true}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if resp.Matched != 1 {
				t.Errorf("Expected 1 old file, got: %d", resp.Matched)
			}
			if !findHasFile(resp.Files, "old.txt") {
				t.Errorf("Should find old.txt")
			}
		})

		t.Run("find files newer than 1 week", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+ageDir+`"],"age":"-1w"}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if resp.Matched != 1 {
				t.Errorf("Expected 1 new file, got: %d", resp.Matched)
			}
			if !findHasFile(resp.Files, "new.txt") {
				t.Errorf("Should find new.txt")
			}
		})

		t.Run("find by size with checksum", func(t *testing.T) {
			remoteExec(t, client, "echo 'hello world' > "+ageDir+"/new.txt")

			resp := getFindResult(t, client, `{"paths":["`+ageDir+`"],"size":"5","hidden":true,"get_checksum":true}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if resp.Matched != 1 {
				t.Errorf("Expected 1 file larger than 5 bytes, got: %d", resp.Matched)
			}
			if !findHasFile(resp.Files, "new.txt") {
				t.Errorf("Should find new.txt")
			}
			if len(resp.Files) > 0 && resp.Files[0].Checksum == "" {
				t.Errorf("Should include checksum")
			}
		})

		t.Run("find by negative size with any type", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+ageDir+`"],"size":"-5","hidden":true,"get_checksum":true,"file_type":"any"}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if !findHasFile(resp.Files, "old.txt") {
				t.Errorf("Should find old.txt")
			}
			if !findHasFile(resp.Files, ".hidden.txt") {
				t.Errorf("Should find .hidden.txt")
			}
		})
	})

	// Contains tests
	t.Run("contains content matching", func(t *testing.T) {
		containsDir := testDir + "/contains"
		remoteExec(t, client, "mkdir -p "+containsDir)

		remoteExec(t, client, fmt.Sprintf("printf 'This is a KO\\nline2\\nOK' > %s/a.txt", containsDir))
		remoteExec(t, client, fmt.Sprintf("printf 'This file has\\na few lines\\nin it' > %s/log.txt", containsDir))

		t.Run("read_whole_file dollar matches end of file", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+containsDir+`"],"patterns":["*.txt"],"contains":"KO$","read_whole_file":true}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
		})

		t.Run("read_whole_file match end of file", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+containsDir+`"],"patterns":["*.txt"],"contains":"OK","read_whole_file":true}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if !findHasFile(resp.Files, "a.txt") {
				t.Errorf("Should find a.txt containing OK")
			}
		})

		t.Run("line-by-line contains matching", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+containsDir+`"],"patterns":["*.txt"],"contains":".*KO"}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if !findHasFile(resp.Files, "a.txt") {
				t.Errorf("Should find a.txt")
			}
		})

		t.Run("read_whole_file match across line boundaries", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+containsDir+`"],"patterns":["*.txt"],"contains":"has\\na few","read_whole_file":true}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if !findHasFile(resp.Files, "log.txt") {
				t.Errorf("Should find log.txt with cross-line match")
			}
		})

		t.Run("line-by-line no match across lines", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+containsDir+`"],"patterns":["*.txt"],"contains":"has\\na few"}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if resp.Matched != 0 {
				t.Errorf("Expected 0 matches for cross-line pattern in line mode, got: %d", resp.Matched)
			}
		})
	})

	// Hidden files test
	t.Run("hidden files", func(t *testing.T) {
		hiddenDir := testDir + "/hidden_test"
		remoteExec(t, client, "mkdir -p "+hiddenDir)
		remoteExec(t, client, "touch "+hiddenDir+"/visible.txt")
		remoteExec(t, client, "touch "+hiddenDir+"/.hidden.txt")

		t.Run("without hidden flag", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+hiddenDir+`"]}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if resp.Matched != 1 {
				t.Errorf("Expected 1 visible file, got: %d", resp.Matched)
			}
		})

		t.Run("with hidden flag", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+hiddenDir+`"],"hidden":true}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if resp.Matched != 2 {
				t.Errorf("Expected 2 files (visible + hidden), got: %d", resp.Matched)
			}
		})
	})

	// Nonexistent path test
	t.Run("nonexistent path", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["/tmp/idontexist_dibra_test_xyz"]}`)
		if resp.Matched != 0 {
			t.Errorf("Expected 0 matches for nonexistent path, got: %d", resp.Matched)
		}
		if len(resp.SkippedPaths) == 0 {
			t.Errorf("Expected skipped_paths to include the path")
		}
	})

	// Glob pattern exclusion test
	t.Run("exclude with glob patterns", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`"],"recurse":true,"excludes":["*.jpg","*.ogg"]}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if findHasFile(resp.Files, "2.jpg") {
			t.Errorf("Should not find jpg file")
		}
		if findHasFile(resp.Files, "8.ogg") {
			t.Errorf("Should not find ogg file")
		}
	})

	// Limit test
	t.Run("limit matches", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`"],"recurse":true,"limit":3}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if resp.Matched != 3 {
			t.Errorf("Expected exactly 3 matches with limit, got: %d", resp.Matched)
		}
	})

	// Symlink tests
	t.Run("find symlinks", func(t *testing.T) {
		linkDir := testDir + "/link_test"
		remoteExec(t, client, "mkdir -p "+linkDir)
		remoteExec(t, client, "echo 'target' > "+linkDir+"/target.txt")
		remoteExec(t, client, "ln -sf "+linkDir+"/target.txt "+linkDir+"/link.txt")

		t.Run("file_type link", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+linkDir+`"],"file_type":"link"}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if resp.Matched != 1 {
				t.Errorf("Expected 1 symlink, got: %d", resp.Matched)
			}
			if !findHasFile(resp.Files, "link.txt") {
				t.Errorf("Should find link.txt")
			}
		})

		t.Run("file_type file finds only regular", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+linkDir+`"],"file_type":"file"}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if resp.Matched != 1 {
				t.Errorf("Expected 1 regular file, got: %d", resp.Matched)
			}
			if !findHasFile(resp.Files, "target.txt") {
				t.Errorf("Should find target.txt")
			}
		})
	})

	// Mode filtering tests
	t.Run("mode filtering", func(t *testing.T) {
		modeDir := testDir + "/mode_test"
		remoteExec(t, client, "mkdir -p "+modeDir)
		remoteExec(t, client, "touch "+modeDir+"/mode_0644")
		remoteExec(t, client, "chmod 0644 "+modeDir+"/mode_0644")
		remoteExec(t, client, "touch "+modeDir+"/mode_0444")
		remoteExec(t, client, "chmod 0444 "+modeDir+"/mode_0444")
		remoteExec(t, client, "touch "+modeDir+"/mode_0400")
		remoteExec(t, client, "chmod 0400 "+modeDir+"/mode_0400")
		remoteExec(t, client, "touch "+modeDir+"/mode_0700")
		remoteExec(t, client, "chmod 0700 "+modeDir+"/mode_0700")
		remoteExec(t, client, "touch "+modeDir+"/mode_0666")
		remoteExec(t, client, "chmod 0666 "+modeDir+"/mode_0666")

		t.Run("exact mode 0644", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+modeDir+`"],"patterns":["mode_*"],"mode":"0644","exact_mode":true}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if resp.Matched != 1 {
				t.Errorf("Expected 1 file with mode 0644, got: %d", resp.Matched)
			}
			if !findHasFile(resp.Files, "mode_0644") {
				t.Errorf("Should find mode_0644")
			}
		})

		t.Run("minimum mode user readable", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+modeDir+`"],"patterns":["mode_*"],"mode":"0400","exact_mode":false}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if resp.Matched != 5 {
				t.Errorf("Expected 5 files with at least user-read, got: %d", resp.Matched)
			}
		})

		t.Run("minimum mode other readable", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+modeDir+`"],"patterns":["mode_*"],"mode":"0004","exact_mode":false}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			// 0444, 0644, 0666 have other read
			if resp.Matched != 3 {
				t.Errorf("Expected 3 files readable by others, got: %d", resp.Matched)
			}
		})
	})

	// Checksum algorithm tests
	t.Run("checksum algorithms", func(t *testing.T) {
		checksumDir := testDir + "/checksum_test"
		remoteExec(t, client, "mkdir -p "+checksumDir)
		remoteExec(t, client, "echo -n 'test data' > "+checksumDir+"/test.txt")

		t.Run("sha1 checksum", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+checksumDir+`"],"get_checksum":true,"checksum_algorithm":"sha1"}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if len(resp.Files) == 0 || resp.Files[0].Checksum == "" {
				t.Errorf("Should include checksum")
			}
		})

		t.Run("md5 checksum", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+checksumDir+`"],"get_checksum":true,"checksum_algorithm":"md5"}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if len(resp.Files) == 0 || resp.Files[0].Checksum == "" {
				t.Errorf("Should include checksum")
			}
		})

		t.Run("sha256 checksum", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+checksumDir+`"],"get_checksum":true,"checksum_algorithm":"sha256"}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if len(resp.Files) == 0 || resp.Files[0].Checksum == "" {
				t.Errorf("Should include checksum")
			}
		})
	})

	// Multiple paths test
	t.Run("multiple paths", func(t *testing.T) {
		multiDir1 := testDir + "/multi1"
		multiDir2 := testDir + "/multi2"
		remoteExec(t, client, "mkdir -p "+multiDir1)
		remoteExec(t, client, "mkdir -p "+multiDir2)
		remoteExec(t, client, "echo 'one' > "+multiDir1+"/file1.txt")
		remoteExec(t, client, "echo 'two' > "+multiDir2+"/file2.txt")

		resp := getFindResult(t, client, `{"paths":["`+multiDir1+`","`+multiDir2+`"],"patterns":["*.txt"]}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if resp.Matched != 2 {
			t.Errorf("Expected 2 files from multiple paths, got: %d", resp.Matched)
		}
	})

	// Age with different units test
	t.Run("age with different units", func(t *testing.T) {
		ageUnitsDir := testDir + "/age_units"
		remoteExec(t, client, "mkdir -p "+ageUnitsDir)
		remoteExec(t, client, "touch "+ageUnitsDir+"/recent.txt")
		remoteExec(t, client, "touch -t 202001011200 "+ageUnitsDir+"/old.txt")

		t.Run("age in seconds", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+ageUnitsDir+`"],"age":"3600"}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if !findHasFile(resp.Files, "old.txt") {
				t.Errorf("Should find old.txt")
			}
		})

		t.Run("age in days", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+ageUnitsDir+`"],"age":"1d"}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if !findHasFile(resp.Files, "old.txt") {
				t.Errorf("Should find old.txt")
			}
		})

		t.Run("age in minutes", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+ageUnitsDir+`"],"age":"60m"}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
		})
	})

	// Size with different units test
	t.Run("size with different units", func(t *testing.T) {
		sizeDir := testDir + "/size_test"
		remoteExec(t, client, "mkdir -p "+sizeDir)
		remoteExec(t, client, "dd if=/dev/zero of="+sizeDir+"/small.bin bs=100 count=1 2>/dev/null")
		remoteExec(t, client, "dd if=/dev/zero of="+sizeDir+"/large.bin bs=1024 count=10 2>/dev/null")

		t.Run("size in bytes", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+sizeDir+`"],"size":"500"}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if !findHasFile(resp.Files, "large.bin") {
				t.Errorf("Should find large.bin")
			}
		})

		t.Run("size in kilobytes", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+sizeDir+`"],"size":"1k"}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if !findHasFile(resp.Files, "large.bin") {
				t.Errorf("Should find large.bin")
			}
		})

		t.Run("negative size - smaller files", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+sizeDir+`"],"size":"-500"}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if !findHasFile(resp.Files, "small.bin") {
				t.Errorf("Should find small.bin")
			}
		})
	})

	// Combined criteria test
	t.Run("combined criteria", func(t *testing.T) {
		comboDir := testDir + "/combo"
		remoteExec(t, client, "mkdir -p "+comboDir)
		remoteExec(t, client, "echo 'short' > "+comboDir+"/small.log")
		remoteExec(t, client, "dd if=/dev/zero bs=1024 count=100 2>/dev/null | tr '\\0' 'x' > "+comboDir+"/large.log")
		remoteExec(t, client, "echo 'short' > "+comboDir+"/small.txt")
		remoteExec(t, client, "touch -t 202001011200 "+comboDir+"/small.log")

		resp := getFindResult(t, client, `{"paths":["`+comboDir+`"],"patterns":["*.log"],"age":"1w","size":"10"}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		t.Logf("Combined criteria: matched=%d", resp.Matched)
	})

	// Recursive hidden directory test
	t.Run("hidden directory recursion", func(t *testing.T) {
		hiddenRecDir := testDir + "/hidden_rec"
		remoteExec(t, client, "mkdir -p "+hiddenRecDir+"/.hidden_dir")
		remoteExec(t, client, "touch "+hiddenRecDir+"/.hidden_dir/inside.txt")
		remoteExec(t, client, "touch "+hiddenRecDir+"/visible.txt")

		t.Run("without hidden skips hidden dirs", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+hiddenRecDir+`"],"recurse":true}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if resp.Matched != 1 {
				t.Errorf("Expected 1 visible file only, got: %d", resp.Matched)
			}
		})

		t.Run("with hidden includes hidden dirs", func(t *testing.T) {
			resp := getFindResult(t, client, `{"paths":["`+hiddenRecDir+`"],"recurse":true,"hidden":true}`)
			if resp.Failed {
				t.Fatalf("Expected success, got failed: %s", resp.Msg)
			}
			if !findHasFile(resp.Files, "inside.txt") {
				t.Errorf("Should find inside.txt in hidden dir")
			}
		})
	})

	// path alias test - uses runPlaybook to test controller alias
	t.Run("path alias", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Find using path alias
    find:
      path:
        - ` + testDir + `/a
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success with path alias, got: %s", output)
		}
	})

	// Return value structure test
	t.Run("return value structure", func(t *testing.T) {
		statDir := testDir + "/return_struct"
		remoteExec(t, client, "mkdir -p "+statDir)
		remoteExec(t, client, "echo -n 'hello' > "+statDir+"/test.txt")

		resp := getFindResult(t, client, `{"paths":["`+statDir+`"],"patterns":["*.txt"]}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if resp.Msg == "" {
			t.Errorf("Expected msg to be set")
		}
		if resp.Matched != 1 {
			t.Errorf("Expected matched=1, got: %d", resp.Matched)
		}
		if resp.Examined < 1 {
			t.Errorf("Expected examined >= 1, got: %d", resp.Examined)
		}
		if len(resp.Files) != 1 {
			t.Fatalf("Expected 1 file, got: %d", len(resp.Files))
		}
		f := resp.Files[0]
		if !strings.HasSuffix(f.Path, "/test.txt") {
			t.Errorf("Expected path to end with /test.txt, got: %s", f.Path)
		}
		if f.Mode == "" {
			t.Errorf("Expected mode to be set")
		}
		if !f.IsReg {
			t.Errorf("Expected isreg=true")
		}
		if f.IsDir {
			t.Errorf("Expected isdir=false")
		}
		if f.Size != 5 {
			t.Errorf("Expected size=5, got: %d", f.Size)
		}
	})

	// Idempotency test - uses runPlaybook
	t.Run("idempotency", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Find files idempotency check
    find:
      paths:
        - ` + testDir + `/a
      patterns:
        - "*.txt"
`
		output1 := runPlaybook(t, playbook)
		if strings.Contains(output1, "FAILED") {
			t.Fatalf("First run failed: %s", output1)
		}

		output2 := runPlaybook(t, playbook)
		if strings.Contains(output2, "FAILED") {
			t.Fatalf("Second run failed: %s", output2)
		}

		if strings.Contains(output1, "CHANGED") || strings.Contains(output2, "CHANGED") {
			t.Error("Find should never report changes (read-only module)")
		}
	})

	// template variable test - uses runPlaybook (tests controller)
	t.Run("template variable in path", func(t *testing.T) {
		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass
    become: true

vars:
  find_dir: ` + testDir + `/a

tasks:
  - name: Find using variable path
    find:
      paths:
        - "{{ find_dir }}"
      patterns:
        - "*.txt"
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success with template variable, got: %s", output)
		}
	})

	// find directories with patterns
	t.Run("find directories with pattern", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`"],"file_type":"directory","patterns":["a"],"recurse":true}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if resp.Matched != 1 {
			t.Errorf("Expected 1 directory matching 'a', got: %d", resp.Matched)
		}
	})

	// Empty directory test
	t.Run("empty directory", func(t *testing.T) {
		emptyDir := testDir + "/empty"
		remoteExec(t, client, "mkdir -p "+emptyDir)

		resp := getFindResult(t, client, `{"paths":["`+emptyDir+`"]}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if resp.Matched != 0 {
			t.Errorf("Expected 0 matches in empty dir, got: %d", resp.Matched)
		}
	})

	// File stat info verification
	t.Run("stat info verification", func(t *testing.T) {
		statDir := testDir + "/stat_verify"
		remoteExec(t, client, "mkdir -p "+statDir)
		remoteExec(t, client, "echo -n 'hello' > "+statDir+"/test.txt")
		remoteExec(t, client, "chmod 0644 "+statDir+"/test.txt")

		resp := getFindResult(t, client, `{"paths":["`+statDir+`"]}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if len(resp.Files) != 1 {
			t.Fatalf("Expected 1 file, got: %d", len(resp.Files))
		}
		f := resp.Files[0]
		if !f.IsReg {
			t.Errorf("Expected isreg=true")
		}
		if f.IsDir {
			t.Errorf("Expected isdir=false")
		}
		if f.Mode != "0644" {
			t.Errorf("Expected mode=0644, got: %s", f.Mode)
		}
		if f.Size != 5 {
			t.Errorf("Expected size=5, got: %d", f.Size)
		}
	})

	// Multiple regex patterns test
	t.Run("multiple regex patterns", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`"],"recurse":true,"use_regex":true,"patterns":[".*\\.txt$",".*\\.json$"]}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if !findHasFile(resp.Files, "1.txt") {
			t.Errorf("Should find 1.txt")
		}
		if !findHasFile(resp.Files, "5.json") {
			t.Errorf("Should find 5.json")
		}
	})

	// Follow symlinks test
	t.Run("follow symlinks", func(t *testing.T) {
		followDir := testDir + "/follow_test"
		remoteExec(t, client, "mkdir -p "+followDir+"/real_dir")
		remoteExec(t, client, "echo 'data' > "+followDir+"/real_dir/file.txt")
		remoteExec(t, client, "ln -sf "+followDir+"/real_dir "+followDir+"/sym_dir")

		resp := getFindResult(t, client, `{"paths":["`+followDir+`"],"recurse":true,"follow":true}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if !findHasFile(resp.Files, "file.txt") {
			t.Errorf("Should find file.txt when following symlinks")
		}
	})

	// Contains with file_type directory should be ignored
	t.Run("contains only applies to files", func(t *testing.T) {
		containsDir2 := testDir + "/contains2"
		remoteExec(t, client, "mkdir -p "+containsDir2)
		remoteExec(t, client, "echo 'searchme' > "+containsDir2+"/has_content.txt")
		remoteExec(t, client, "echo 'other' > "+containsDir2+"/no_content.txt")

		resp := getFindResult(t, client, `{"paths":["`+containsDir2+`"],"contains":"searchme"}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if resp.Matched != 1 {
			t.Errorf("Expected 1 file matching contains, got: %d", resp.Matched)
		}
		if !findHasFile(resp.Files, "has_content.txt") {
			t.Errorf("Should find has_content.txt")
		}
	})

	// Age stamp test (ctime vs mtime)
	t.Run("age_stamp ctime", func(t *testing.T) {
		ageStampDir := testDir + "/age_stamp"
		remoteExec(t, client, "mkdir -p "+ageStampDir)
		remoteExec(t, client, "touch "+ageStampDir+"/file.txt")

		resp := getFindResult(t, client, `{"paths":["`+ageStampDir+`"],"age":"-1w","age_stamp":"ctime"}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if resp.Matched != 1 {
			t.Errorf("Expected 1 recent file by ctime, got: %d", resp.Matched)
		}
	})

	// Pattern alias test - uses runPlaybook (tests controller alias)
	t.Run("pattern alias", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Find using pattern alias
    find:
      paths:
        - ` + testDir + `/a
      pattern:
        - "*.txt"
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success with pattern alias, got: %s", output)
		}
	})

	// Exclude alias test - uses runPlaybook (tests controller alias)
	t.Run("exclude alias", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Find using exclude alias
    find:
      paths:
        - ` + testDir + `
      recurse: true
      exclude:
        - "*.ogg"
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Expected success with exclude alias, got: %s", output)
		}
	})

	// Permission denied handling test - uses runPlaybook (different user)
	t.Run("permission denied handling", func(t *testing.T) {
		permDir := testDir + "/perm_test"
		remoteExec(t, client, "mkdir -p "+permDir+"/readable")
		remoteExec(t, client, "mkdir -p "+permDir+"/unreadable")
		remoteExec(t, client, "touch "+permDir+"/readable/file.txt")
		remoteExec(t, client, "touch "+permDir+"/unreadable/file.txt")
		remoteExec(t, client, "chmod 000 "+permDir+"/unreadable")

		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: testuser
    password: testpass

tasks:
  - name: Find with permission issue
    find:
      paths:
        - ` + permDir + `
      recurse: true
`
		output := runPlaybook(t, playbook)
		t.Logf("Permission test output: %s", output)

		// Cleanup
		remoteExec(t, client, "chmod 755 "+permDir+"/unreadable")
	})

	// Large number of files test
	t.Run("many files performance", func(t *testing.T) {
		manyDir := testDir + "/many"
		remoteExec(t, client, "mkdir -p "+manyDir)
		remoteExec(t, client, "for i in $(seq 1 50); do touch "+manyDir+"/file_$i.txt; done")

		resp := getFindResult(t, client, `{"paths":["`+manyDir+`"],"patterns":["*.txt"]}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if resp.Matched != 50 {
			t.Errorf("Expected 50 files, got: %d", resp.Matched)
		}
	})

	// Regex with file_type directory
	t.Run("regex directory patterns", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`"],"file_type":"directory","use_regex":true,"patterns":["^[a-e]$"],"recurse":true}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		// Dirs a, b, c, d, e all match ^[a-e]$
		if resp.Matched != 5 {
			t.Errorf("Expected 5 directories matching regex, got: %d", resp.Matched)
		}
	})

	// Limit with recurse
	t.Run("limit with recursive search", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`"],"recurse":true,"file_type":"any","limit":1}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if resp.Matched != 1 {
			t.Errorf("Expected exactly 1 match with limit=1, got: %d", resp.Matched)
		}
	})

	// File with no extension
	t.Run("file with no extension", func(t *testing.T) {
		resp := getFindResult(t, client, `{"paths":["`+testDir+`/a/b/c"],"patterns":["3"]}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if resp.Matched != 1 {
			t.Errorf("Expected 1 match for file '3', got: %d", resp.Matched)
		}
	})

	// Default patterns match everything
	t.Run("default patterns match all", func(t *testing.T) {
		defaultDir := testDir + "/default_patterns"
		remoteExec(t, client, "mkdir -p "+defaultDir)
		remoteExec(t, client, "touch "+defaultDir+"/a.txt")
		remoteExec(t, client, "touch "+defaultDir+"/b.log")
		remoteExec(t, client, "touch "+defaultDir+"/c")

		resp := getFindResult(t, client, `{"paths":["`+defaultDir+`"]}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if resp.Matched != 3 {
			t.Errorf("Expected 3 files with default pattern, got: %d", resp.Matched)
		}
	})

	// Size not applied to directories
	t.Run("size not applied to directories", func(t *testing.T) {
		sizeDirTest := testDir + "/size_dir_test"
		remoteExec(t, client, "mkdir -p "+sizeDirTest+"/subdir")
		remoteExec(t, client, "touch "+sizeDirTest+"/subdir/file.txt")

		resp := getFindResult(t, client, `{"paths":["`+sizeDirTest+`"],"file_type":"directory","size":"1m"}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if !findHasFile(resp.Files, "subdir") {
			t.Errorf("Should find subdir (size not applied to dirs)")
		}
	})

	// File type "any" with size filter
	t.Run("any type size only on files", func(t *testing.T) {
		anyDir := testDir + "/any_size"
		remoteExec(t, client, "mkdir -p "+anyDir+"/subdir")
		remoteExec(t, client, "echo -n 'tiny' > "+anyDir+"/small.txt")
		remoteExec(t, client, "dd if=/dev/zero of="+anyDir+"/big.bin bs=1024 count=2 2>/dev/null")

		resp := getFindResult(t, client, `{"paths":["`+anyDir+`"],"file_type":"any","size":"1k"}`)
		if resp.Failed {
			t.Fatalf("Expected success, got failed: %s", resp.Msg)
		}
		if !findHasFile(resp.Files, "subdir") {
			t.Errorf("Should find subdir (dirs not filtered by size)")
		}
		if !findHasFile(resp.Files, "big.bin") {
			t.Errorf("Should find big.bin")
		}
		if findHasFile(resp.Files, "small.txt") {
			t.Errorf("Should NOT find small.txt (too small)")
		}
	})
}
