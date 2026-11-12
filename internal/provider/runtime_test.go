package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// fakeRuntimes is Cloud with a runtime named ci on two servers, and one made
// here, its ingress turned off and on.
type fakeRuntimes struct {
	mu   sync.Mutex
	list []map[string]any
	sent []map[string]any
}

func (f *fakeRuntimes) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/v1/cloud/runtimes":
		reply(200, f.list)
	case r.Method == "POST" && r.URL.Path == "/v1/cloud/runtimes":
		f.sent = append(f.sent, in)
		rt := map[string]any{"id": "rt9", "name": in["name"], "stack": in["stack"], "server_id": in["server_id"],
			"ingress_enabled": in["ingress_enabled"] != false, "status": "pending"}
		f.list = append(f.list, rt)
		reply(201, rt)
	case r.URL.Path == "/v1/cloud/runtimes/rt9":
		rt := f.list[len(f.list)-1]
		if r.Method == "PATCH" {
			f.sent = append(f.sent, in)
			if v, ok := in["ingress_enabled"].(bool); ok {
				rt["ingress_enabled"] = v
			}
		}
		if r.Method == "DELETE" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		reply(200, rt)
	default:
		reply(404, map[string]string{"error": "No such thing."})
	}
}

func TestRuntimesByServerAndTheirIngress(t *testing.T) {
	f := &fakeRuntimes{list: []map[string]any{
		{"id": "rt1", "name": "ci", "stack": "local:traefik+k3s", "server_id": "sv1", "ingress_enabled": true},
		{"id": "rt2", "name": "ci", "stack": "local:traefik+k3s", "server_id": "sv2", "ingress_enabled": false},
	}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	runtime := func(ingress string) string {
		return cloudProvider(srv.URL) + `
resource "yet_cloud_runtime" "runners" {
  name            = "runners"
  stack           = "local:traefik+k3s"
  server_id       = "sv1"
  ingress_enabled = ` + ingress + `
}
`
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      cloudProvider(srv.URL) + `data "yet_cloud_runtime" "ci" { name = "ci" }`,
				ExpectError: regexp.MustCompile(`several servers`),
			},
			{
				Config: cloudProvider(srv.URL) + `data "yet_cloud_runtime" "ci" {
  name      = "ci"
  server_id = "sv2"
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.yet_cloud_runtime.ci", tfjsonpath.New("id"), knownvalue.StringExact("rt2")),
					statecheck.ExpectKnownValue("data.yet_cloud_runtime.ci", tfjsonpath.New("ingress_enabled"), knownvalue.Bool(false)),
				},
			},
			{
				Config: runtime("false"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_runtime.runners", tfjsonpath.New("ingress_enabled"), knownvalue.Bool(false)),
				},
			},
			{
				Config: runtime("true"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_runtime.runners", tfjsonpath.New("ingress_enabled"), knownvalue.Bool(true)),
				},
			},
		},
	})
	if f.sent[0]["ingress_enabled"] != false || f.sent[len(f.sent)-1]["ingress_enabled"] != true {
		t.Errorf("made with its ingress off, then turned on: %v", f.sent)
	}
}
