package docker

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/moby/moby/api/types/registry"
)

func TestEncodeRegistryAuthForImage(t *testing.T) {
	encoded, err := EncodeRegistryAuthForImage("registry.example.test:5000/team/app", "user", "pass")
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var auth registry.AuthConfig
	if err := json.Unmarshal(data, &auth); err != nil {
		t.Fatal(err)
	}
	if auth.Username != "user" || auth.Password != "pass" || auth.ServerAddress != "registry.example.test:5000" {
		t.Fatalf("decoded auth = %#v", auth)
	}
}

func TestRegistryAuthFromConfig(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("config-user:config-pass"))
	config := []byte(`{"auths":{"https://index.docker.io/v1/":{"auth":"` + encoded + `"}}}`)
	auth, found, err := RegistryAuthFromConfig(config, "alpine:latest")
	if err != nil {
		t.Fatal(err)
	}
	if !found || auth.Username != "config-user" || auth.Password != "config-pass" {
		t.Fatalf("RegistryAuthFromConfig() = %#v, %t", auth, found)
	}
}

func TestRegistryAuthFromConfigIdentityToken(t *testing.T) {
	config := []byte(`{"auths":{"registry.example.test":{"identitytoken":"token"}}}`)
	auth, found, err := RegistryAuthFromConfig(config, "registry.example.test/team/app")
	if err != nil || !found || auth.IdentityToken != "token" {
		t.Fatalf("RegistryAuthFromConfig() = %#v, %t, %v", auth, found, err)
	}
}
