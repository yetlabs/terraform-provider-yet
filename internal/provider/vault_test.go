package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// fakeVault is Vault in Cloud: what values it was sent, never answering one.
type fakeVault struct {
	mu      sync.Mutex
	vault   map[string]any
	secret  map[string]any
	granted bool
	values  []string
}

func (f *fakeVault) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	view := func() map[string]any {
		v := map[string]any{}
		for k, x := range f.vault {
			v[k] = x
		}
		v["applications"] = []string{}
		if f.granted {
			v["applications"] = []string{"app1"}
		}
		return v
	}
	p := strings.TrimPrefix(r.URL.Path, "/v1/vault/vaults")
	switch {
	case r.Method == "POST" && p == "":
		f.vault = map[string]any{"id": "vlt1", "kind": in["kind"], "name": in["name"], "description": "", "aws_account_id": "aws_1", "region": "sa-east-1", "bucket": "yet-acme-vault"}
		reply(201, view())
	case f.vault != nil && p == "/vlt1" && r.Method == "GET":
		reply(200, view())
	case f.vault != nil && p == "/vlt1" && r.Method == "DELETE":
		f.vault = nil
		w.WriteHeader(204)
	case p == "/vlt1/secrets" && r.Method == "POST":
		f.values = append(f.values, in["value"].(string))
		f.secret = map[string]any{"id": "sec1", "vault_id": "vlt1", "name": in["name"], "note": "", "version": "v1"}
		reply(201, f.secret)
	case f.secret != nil && p == "/vlt1/secrets/sec1" && r.Method == "GET":
		reply(200, f.secret)
	case f.secret != nil && p == "/vlt1/secrets/sec1" && r.Method == "PATCH":
		if v, ok := in["value"].(string); ok {
			f.values = append(f.values, v)
			f.secret["version"] = "v2"
		}
		reply(200, f.secret)
	case f.secret != nil && p == "/vlt1/secrets/sec1" && r.Method == "DELETE":
		f.secret = nil
		w.WriteHeader(204)
	case p == "/vlt1/applications" && r.Method == "POST":
		f.granted = true
		reply(201, map[string]any{})
	case p == "/vlt1/applications/app1" && r.Method == "DELETE":
		f.granted = false
		w.WriteHeader(204)
	default:
		reply(404, map[string]string{"error": "No such thing."})
	}
}

// A vault, a secret of it, and an application let read it: the value sent, never read back; changed, sent again.
func TestAVault(t *testing.T) {
	f := &fakeVault{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	config := func(value string) string {
		return cloudProvider(srv.URL) + `
resource "yet_vault_vault" "systems" {
  name = "Systems"
}

resource "yet_vault_secret" "auth" {
  vault_id = yet_vault_vault.systems.id
  name     = "AUTH_SECRET"
  value    = "` + value + `"
}

resource "yet_vault_grant" "api" {
  vault_id       = yet_vault_vault.systems.id
  application_id = "app1"
}
`
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config("s3cret"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_vault_vault.systems", tfjsonpath.New("bucket"), knownvalue.StringExact("yet-acme-vault")),
					statecheck.ExpectKnownValue("yet_vault_vault.systems", tfjsonpath.New("kind"), knownvalue.StringExact("systems")),
					statecheck.ExpectKnownValue("yet_vault_secret.auth", tfjsonpath.New("version"), knownvalue.StringExact("v1")),
					statecheck.ExpectKnownValue("yet_vault_grant.api", tfjsonpath.New("id"), knownvalue.StringExact("vlt1/app1")),
				},
			},
			{
				Config: config("n3w"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_vault_secret.auth", tfjsonpath.New("version"), knownvalue.StringExact("v2")),
				},
			},
		},
	})
	if strings.Join(f.values, ",") != "s3cret,n3w" {
		t.Errorf("its values, each once: %v", f.values)
	}
	if f.vault != nil || f.secret != nil || f.granted {
		t.Error("all of it gone when destroyed")
	}
}
