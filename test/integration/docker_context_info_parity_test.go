//go:build integration

package integration

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestPlaybook_DockerContextInfoParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	mustRemote(t, client, "rm -f /tmp/dibra-context-*.json /tmp/.dibra-agent")
	templatePath := writeResultTemplate(t, "context_result")
	runInfo := func(testName, arguments string) map[string]any {
		t.Helper()
		remotePath := "/tmp/dibra-context-" + testName + ".json"
		playbook := playbookHeader + `
  - name: Inspect docker contexts
    community.docker.docker_context_info:
` + arguments + `
    register: context_result

  - name: Persist context result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("%s context info playbook failed: %s", testName, output)
		}
		return readRemoteJSONMap(t, client, remotePath)
	}

	t.Run("lists default context", func(t *testing.T) {
		result := runInfo("default", "")
		if result["changed"] != false || result["current_context_name"] == nil {
			t.Fatalf("result = %#v", result)
		}
		contexts, _ := result["contexts"].([]any)
		if len(contexts) == 0 {
			t.Fatalf("expected default context: %#v", result)
		}
		foundDefault := false
		for _, item := range contexts {
			context, _ := item.(map[string]any)
			if context["name"] == "default" {
				foundDefault = true
				if context["description"] != "Current DOCKER_HOST based configuration" {
					t.Fatalf("default description = %#v", context)
				}
				if context["meta_path"] != nil || context["tls_path"] != nil {
					t.Fatalf("default paths should be null: %#v", context)
				}
				config, _ := context["config"].(map[string]any)
				if config["docker_host"] == nil {
					t.Fatalf("default config = %#v", config)
				}
			}
		}
		if !foundDefault {
			t.Fatalf("default context missing: %#v", result)
		}
	})

	t.Run("only_current returns one context", func(t *testing.T) {
		result := runInfo("current", "      only_current: true\n")
		contexts, _ := result["contexts"].([]any)
		if len(contexts) != 1 {
			t.Fatalf("contexts = %#v", result["contexts"])
		}
		context, _ := contexts[0].(map[string]any)
		if context["current"] != true {
			t.Fatalf("current context = %#v", context)
		}
	})

	t.Run("named context preserves defaults nullable metadata and TLS discovery", func(t *testing.T) {
		name := "dibra-context-tls"
		sum := sha256.Sum256([]byte(name))
		id := hex.EncodeToString(sum[:])
		metaDirectory := "/root/.docker/contexts/meta/" + id
		tlsDirectory := "/root/.docker/contexts/tls/" + id + "/docker"
		metadata := `{"Name":"` + name + `","Metadata":{"Description":null},"Endpoints":{"docker":{}}}`
		encodedMetadata := base64.StdEncoding.EncodeToString([]byte(metadata))
		mustRemote(t, client, "mkdir -p "+metaDirectory+" "+tlsDirectory+
			"; echo "+encodedMetadata+" | base64 -d > "+metaDirectory+"/meta.json"+
			"; printf ca > "+tlsDirectory+"/ca-context.crt"+
			"; printf cert > "+tlsDirectory+"/cert-context.crt"+
			"; printf key > "+tlsDirectory+"/key-context.key")
		defer mustRemote(t, client, "rm -rf "+metaDirectory+" /root/.docker/contexts/tls/"+id)

		result := runInfo("named-tls", "      name: "+name+"\n")
		contexts, _ := result["contexts"].([]any)
		if len(contexts) != 1 {
			t.Fatalf("contexts = %#v", result)
		}
		context, _ := contexts[0].(map[string]any)
		if context["description"] != nil || context["meta_path"] != metaDirectory || context["tls_path"] != "/root/.docker/contexts/tls/"+id {
			t.Fatalf("context = %#v", context)
		}
		config, _ := context["config"].(map[string]any)
		if config["docker_host"] != "unix:///var/run/docker.sock" ||
			config["tls"] != true ||
			config["ca_path"] != tlsDirectory+"/ca-context.crt" ||
			config["client_cert"] != tlsDirectory+"/cert-context.crt" ||
			config["client_key"] != tlsDirectory+"/key-context.key" {
			t.Fatalf("config = %#v", config)
		}
		validateCerts, found := config["validate_certs"]
		if !found || validateCerts != nil {
			t.Fatalf("validate_certs = %#v (found %t), config = %#v", validateCerts, found, config)
		}
	})

	t.Run("missing named context fails", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Inspect missing context
    community.docker.docker_context_info:
      name: definitely-missing-dibra-context
`
		output := runPlaybook(t, playbook)
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "There is no context of name") {
			t.Fatalf("missing context output = %s", output)
		}
	})

	t.Run("cli_context override", func(t *testing.T) {
		result := runInfo("cli", "      only_current: true\n      cli_context: default\n")
		if result["current_context_name"] != "default" {
			t.Fatalf("result = %#v", result)
		}
	})
}
