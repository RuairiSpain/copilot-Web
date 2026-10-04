package validate

import "testing"

func TestKeyVaultReferences(t *testing.T) {
	for value, want := range map[string]bool{
		"@Microsoft.KeyVault(SecretUri=https://v.vault.azure.net/secrets/x/)": true,
		"keyvault:my-secret":                           true,
		"https://my-vault.vault.azure.net/secrets/key": true,
		"my-secret":           false,
		"hunter2 with spaces": false,
	} {
		if got := IsKeyVaultReference(value); got != want {
			t.Errorf("IsKeyVaultReference(%q) = %v", value, got)
		}
	}
}

func TestSecretNamesOrReferences(t *testing.T) {
	for value, want := range map[string]bool{
		"db-password": true, "keyvault:db-password": true, "has spaces!": false,
		"x" + string(make([]byte, 200)): false,
	} {
		if got := IsSecretNameOrReference(value); got != want {
			t.Errorf("IsSecretNameOrReference(%q) = %v", value, got)
		}
	}
	for _, v := range []string{"x1234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890"} {
		if IsSecretNameOrReference(v) {
			t.Error("names over 127 characters are not Key Vault secret names")
		}
	}
}

func TestRawSecretKind(t *testing.T) {
	if rawSecretKind("hello world") != "" || rawSecretKind("https://example.com/path") != "" {
		t.Fatal("false positive")
	}
	if got := rawSecretKind("DefaultEndpointsProtocol=https;AccountKey=abc"); got != "a storage or bus key" {
		t.Fatal(got)
	}
}

func TestRegions(t *testing.T) {
	if !isKnownRegion("westeurope") || !isKnownRegion("West Europe") || isKnownRegion("mars") {
		t.Fatal("known regions")
	}
	if canonicalRegion(" East US 2 ") != "eastus2" {
		t.Fatal("canonicalRegion")
	}
}

func TestPrivateRanges(t *testing.T) {
	for value, want := range map[string]bool{
		"10.0.0.0/8": true, "10.1.2.3": true, "172.16.0.0/12": true, "172.32.0.0/16": false, "192.168.5.0/24": true,
		"100.64.0.0/10": true, "fd00::/8": true, "203.0.113.0/24": false, "8.8.8.8": false, "not-an-ip": false,
		"0.0.0.0/0": false,
	} {
		if got := isPrivateRange(value); got != want {
			t.Errorf("isPrivateRange(%q) = %v", value, got)
		}
	}
	if _, ok := parseNet("garbage"); ok {
		t.Fatal("parseNet accepted garbage")
	}
}

func TestFloatingImages(t *testing.T) {
	for image, want := range map[string]bool{
		"":                           false,
		"ghcr.io/x/y":                true,
		"ghcr.io/x/y:latest":         true,
		"ghcr.io/x/y:":               true,
		"ghcr.io/x/y:1.2":            false,
		"localhost:5000/y":           true,
		"localhost:5000/y:1":         false,
		"ghcr.io/x/y@sha256:abcdef0": false,
	} {
		if got := floatingImage(image); got != want {
			t.Errorf("floatingImage(%q) = %v", image, got)
		}
	}
}

func TestPaths(t *testing.T) {
	if path() != "x-foundry" || path("a", "b[c]") != "x-foundry.a.b[c]" {
		t.Fatal("path")
	}
	for scope, want := range map[string]string{"root": "x-foundry", "hub": "x-foundry.hub", "project:fin": "x-foundry.projects[fin]"} {
		if got := scopePath(scope); got != want {
			t.Errorf("scopePath(%q) = %q", scope, got)
		}
	}
	if got := itemPath("hub", "knowledgeBases", "kb", "x"); got != "x-foundry.hub.iq.knowledgeBases[kb].x" {
		t.Fatal(got)
	}
	if got := pointerPath([]string{"a", "0", "b"}); got != "x-foundry.a[0].b" {
		t.Fatal(got)
	}
	if hasPrefixFold("ABC/def", "abc/") != true || hasPrefixFold("ab", "abc") {
		t.Fatal("hasPrefixFold")
	}
}
