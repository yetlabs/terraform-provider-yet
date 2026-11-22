package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// factories are the provider, as Terraform runs it in tests.
var factories = map[string]func() (tfprotov6.ProviderServer, error){
	"yet": providerserver.NewProtocol6WithError(New("test")()),
}

// Every schema as Terraform asks for it: the framework checks each one here.
func TestTheSchemasHold(t *testing.T) {
	server, err := factories["yet"]()
	if err != nil {
		t.Fatal(err)
	}
	got, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range got.Diagnostics {
		t.Errorf("%s: %s", d.Summary, d.Detail)
	}
	if len(got.ResourceSchemas) != 35 || len(got.DataSourceSchemas) != 11 || len(got.ActionSchemas) != 10 {
		t.Errorf("resources %d, data sources %d, actions %d", len(got.ResourceSchemas), len(got.DataSourceSchemas), len(got.ActionSchemas))
	}
}

// accPreCheck is what an acceptance test needs: a key of a real company.
func accPreCheck(t *testing.T) {
	if os.Getenv("YET_API_KEY") == "" {
		t.Fatal("YET_API_KEY must be set for acceptance tests: a key of a company to test in")
	}
}

const testKey = "yet_acme_0123456789abcdef0123456789abcdef0123456789abcdef"

/*
fakeYet is yet as far as unit tests need it — Cloud's namespaces and the
company's apps — kept in memory, so Terraform itself can plan, apply,
import and destroy against it with no company behind.
*/
type fakeYet struct {
	mu         sync.Mutex
	namespaces map[string]map[string]any
	appsOff    []string
	next       int
}

func newFakeYet(t *testing.T) *httptest.Server {
	f := &fakeYet{namespaces: map[string]map[string]any{}, appsOff: []string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeYet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+testKey {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/cloud/namespaces/")
	switch {
	case r.Method == "GET" && r.URL.Path == "/v1/cloud/namespaces":
		list := []map[string]any{}
		for _, ns := range f.namespaces {
			list = append(list, ns)
		}
		reply(200, list)
	case r.Method == "POST" && r.URL.Path == "/v1/cloud/namespaces":
		f.next++
		ns := map[string]any{"id": fmt.Sprintf("ns%d", f.next), "name": in["name"]}
		f.namespaces[ns["id"].(string)] = ns
		reply(201, ns)
	case r.Method == "PATCH" && f.namespaces[id] != nil:
		f.namespaces[id]["name"] = in["name"]
		reply(200, f.namespaces[id])
	case r.Method == "DELETE" && f.namespaces[id] != nil:
		delete(f.namespaces, id)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "GET" && r.URL.Path == "/v1/settings/apps":
		reply(200, map[string]any{"off": f.appsOff})
	case r.Method == "PUT" && r.URL.Path == "/v1/settings/apps":
		f.appsOff = []string{}
		for _, a := range in["off"].([]any) {
			f.appsOff = append(f.appsOff, a.(string))
		}
		reply(200, map[string]any{"appsOff": f.appsOff})
	default:
		reply(404, map[string]string{"error": "No such thing."})
	}
}

// providerFor is the provider's block pointed at a fake yet.
func providerFor(url string) string {
	return `
provider "yet" {
  api_key       = "` + testKey + `"
  cloud_url = "` + url + `"
}
`
}
