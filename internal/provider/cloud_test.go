package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

var (
	cloudResources = []func() resource.Resource{
		newContainerResource, newRunnerResource, newGitHubConnectionResource,
		newApplicationAddonResource, newDatabaseAttachmentResource,
	}
	cloudDataSources = []func() datasource.DataSource{newStacksDataSource, newGitHubRepositoriesDataSource, newGitHubBranchesDataSource}
	cloudActions     = []func() action.Action{newApplicationBuildAction, newApplicationPodDeleteAction, newDNSZoneVerifyAction, newDatabaseAdoptAction}
)

// Cloud's schemas, as Terraform asks for them: the framework checks each one.
func TestCloudSchemasHold(t *testing.T) {
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
	for _, name := range []string{
		"yet_cloud_container", "yet_cloud_runner", "yet_settings_connection_github",
		"yet_cloud_application_addon", "yet_cloud_database_attachment",
	} {
		if got.ResourceSchemas[name] == nil {
			t.Errorf("no resource %s", name)
		}
	}
	// An application destroyed keeps its add-ons unless said.
	if app := got.ResourceSchemas["yet_cloud_application"]; app == nil || !slices.ContainsFunc(app.Block.Attributes, func(a *tfprotov6.SchemaAttribute) bool {
		return a.Name == "addons_on_delete" && a.Optional && a.Computed
	}) {
		t.Error("yet_cloud_application has no addons_on_delete")
	}
	for _, name := range []string{"yet_cloud_stacks", "yet_cloud_usage", "yet_settings_github_repositories", "yet_settings_github_branches"} {
		if got.DataSourceSchemas[name] == nil {
			t.Errorf("no data source %s", name)
		}
	}
	for _, name := range []string{
		"yet_cloud_application_build", "yet_cloud_application_pod_delete", "yet_cloud_dns_zone_verify",
		"yet_cloud_database_adopt",
	} {
		if got.ActionSchemas[name] == nil {
			t.Errorf("no action %s", name)
		}
	}
}

/*
fakeCloud is Cloud as far as these tests need it: a Docker runtime's
containers — their secrets kept, never shown back — and an application's
attachments, in memory.
*/
type fakeCloud struct {
	mu         sync.Mutex
	containers map[string]map[string]any
	attached   map[string]string
	bucket     map[string]any
	chosen     any
	revoked    bool
	next       int
}

func newFakeCloud(t *testing.T) (*fakeCloud, *httptest.Server) {
	f := &fakeCloud{containers: map[string]map[string]any{}, attached: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

// shown is a container with its secrets' values left out.
func shown(ct map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range ct {
		out[k] = v
	}
	env := []any{}
	for _, v := range ct["env"].([]any) {
		e := map[string]any{}
		for k, x := range v.(map[string]any) {
			e[k] = x
		}
		if e["secret"] == true {
			e["value"] = ""
		}
		env = append(env, e)
	}
	out["env"] = env
	return out
}

func (f *fakeCloud) secret(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ct := range f.containers {
		for _, v := range ct["env"].([]any) {
			if e := v.(map[string]any); e["name"] == name && e["secret"] == true {
				return e["value"].(string)
			}
		}
	}
	return ""
}

func (f *fakeCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	const containers, attachments = "/v1/cloud/runtimes/rt1/containers", "/v1/cloud/applications/a1/attachments/"
	id := strings.TrimPrefix(r.URL.Path, containers+"/")
	ct := f.containers[id]
	switch {
	case r.Method == "GET" && r.URL.Path == containers:
		list := []map[string]any{}
		for _, ct := range f.containers {
			list = append(list, shown(ct))
		}
		reply(200, list)
	case r.Method == "POST" && r.URL.Path == containers:
		f.next++
		in["id"], in["runtime_id"], in["state"], in["disabled"] = fmt.Sprintf("ct%d", f.next), "rt1", "pending", false
		f.containers[in["id"].(string)] = in
		reply(201, shown(in))
	case r.Method == "PUT" && ct != nil:
		// A secret sent empty keeps its value.
		old := map[any]any{}
		for _, v := range ct["env"].([]any) {
			old[v.(map[string]any)["name"]] = v.(map[string]any)["value"]
		}
		for _, v := range in["env"].([]any) {
			if e := v.(map[string]any); e["secret"] == true && e["value"] == "" {
				e["value"] = old[e["name"]]
			}
		}
		for _, k := range []string{"name", "image", "command", "env", "privileged", "volumes", "replicas"} {
			ct[k] = in[k]
		}
		reply(200, shown(ct))
	case r.Method == "PATCH" && ct != nil:
		ct["disabled"] = in["enabled"] != true
		ct["state"] = map[bool]string{true: "disabled", false: "pending"}[ct["disabled"].(bool)]
		reply(200, shown(ct))
	case r.Method == "DELETE" && ct != nil:
		delete(f.containers, id)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "POST" && r.URL.Path == "/v1/cloud/applications/a1/attachments":
		as, _ := in["env_var"].(string)
		if as == "" {
			as = "DATABASE_URL"
		}
		f.attached[in["database_id"].(string)] = as
		reply(201, map[string]any{"id": in["database_id"], "attached_as": as})
	case r.Method == "GET" && r.URL.Path == "/v1/cloud/applications/a1/addons":
		list := []map[string]any{{"id": "own", "application_id": "a1", "kind": "redis", "env_var": "REDIS_URL"}}
		for db, as := range f.attached {
			list = append(list, map[string]any{"id": db, "environment_id": "e1", "attached_as": as})
		}
		reply(200, list)
	case r.Method == "POST" && r.URL.Path == "/v1/cloud/applications/a1/addons" && in["kind"] == "s3":
		f.chosen = in["buckets"]
		f.bucket = map[string]any{"id": "b1", "application_id": "a1", "kind": "s3", "env_var": "S3", "placement": "member",
			"buckets": []map[string]any{{"environment_id": "e1", "stage": "production", "aws_account_id": "aws1",
				"name": "yet-acme-api-production-1a2b3c", "region": "sa-east-1", "mode": "native", "identity": "app-1-env-1", "status": "ready"}}}
		reply(201, f.bucket)
	case r.Method == "GET" && r.URL.Path == "/v1/cloud/databases/b1" && f.bucket != nil:
		reply(200, f.bucket)
	case r.Method == "DELETE" && r.URL.Path == "/v1/cloud/applications/a1/addons/b1":
		f.bucket, f.revoked = nil, true
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "DELETE" && f.attached[strings.TrimPrefix(r.URL.Path, attachments)] != "":
		delete(f.attached, strings.TrimPrefix(r.URL.Path, attachments))
		w.WriteHeader(http.StatusNoContent)
	default:
		reply(404, map[string]string{"error": "No such thing."})
	}
}

func cloudProvider(url string) string {
	return `
provider "yet" {
  api_key   = "` + testKey + `"
  cloud_url = "` + url + `"
}
`
}

// A container through its whole life: its secret sent and kept, though never
// shown back; turned off and scaled; imported as <runtime_id>/<id>.
func TestContainerLifecycle(t *testing.T) {
	f, srv := newFakeCloud(t)
	config := func(rest string) string {
		return cloudProvider(srv.URL) + `
resource "yet_cloud_container" "dind" {
  runtime_id = "rt1"
  name       = "dind"
  image      = "docker:27-dind"
  privileged = true
  env        = { DOCKER_TLS_CERTDIR = "" }
  secrets    = { REGISTRY_TOKEN = "s3cret" }
  volumes    = ["cache:/var/lib/docker"]
` + rest + `
}
`
	}
	at := func(attr string) tfjsonpath.Path { return tfjsonpath.New(attr) }
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []tfresource.TestStep{
			{
				Config: config(""),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_container.dind", at("secrets").AtMapKey("REGISTRY_TOKEN"), knownvalue.StringExact("s3cret")),
					statecheck.ExpectKnownValue("yet_cloud_container.dind", at("enabled"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue("yet_cloud_container.dind", at("replicas"), knownvalue.Int64Exact(1)),
					statecheck.ExpectKnownValue("yet_cloud_container.dind", at("state"), knownvalue.StringExact("pending")),
				},
				Check: func(*terraform.State) error {
					if got := f.secret("REGISTRY_TOKEN"); got != "s3cret" {
						return fmt.Errorf("the secret Cloud keeps: %q", got)
					}
					return nil
				},
			},
			{
				Config: config("enabled = false\n  replicas = 3"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_container.dind", at("enabled"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue("yet_cloud_container.dind", at("state"), knownvalue.StringExact("disabled")),
					statecheck.ExpectKnownValue("yet_cloud_container.dind", at("replicas"), knownvalue.Int64Exact(3)),
				},
			},
			{
				ResourceName:      "yet_cloud_container.dind",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return "rt1/" + s.RootModule().Resources["yet_cloud_container.dind"].Primary.ID, nil
				},
				ImportStateVerifyIgnore: []string{"secrets"},
			},
		},
	})
}

// An environment's database attached to an application, in the variable
// asked for — and in the one it has, once imported.
func TestDatabaseAttachment(t *testing.T) {
	_, srv := newFakeCloud(t)
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []tfresource.TestStep{
			{
				Config: cloudProvider(srv.URL) + `
resource "yet_cloud_database_attachment" "ledger" {
  application_id = "a1"
  database_id    = "ledger"
  env_var        = "LEDGER_URL"
}

resource "yet_cloud_database_attachment" "orders" {
  application_id = "a1"
  database_id    = "orders"
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_database_attachment.ledger", tfjsonpath.New("id"), knownvalue.StringExact("a1/ledger")),
					statecheck.ExpectKnownValue("yet_cloud_database_attachment.orders", tfjsonpath.New("env_var"), knownvalue.StringExact("DATABASE_URL")),
				},
			},
			{
				ResourceName:      "yet_cloud_database_attachment.ledger",
				ImportState:       true,
				ImportStateId:     "a1/ledger",
				ImportStateVerify: true,
			},
		},
	})
}

// An S3 bucket as an application's add-on: each stage's shown as Cloud made
// it, and its way to it taken away when destroyed.
func TestBucketAddon(t *testing.T) {
	f, srv := newFakeCloud(t)
	at := tfjsonpath.New("buckets").AtSliceIndex(0)
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			if got := fmt.Sprint(f.chosen); got != "[map[environment_id:e1 existing:true name:acme-assets]]" {
				return fmt.Errorf("the buckets chosen, sent: %s", got)
			}
			if !f.revoked {
				return fmt.Errorf("the bucket's add-on was never removed")
			}
			return nil
		},
		Steps: []tfresource.TestStep{
			{
				Config: cloudProvider(srv.URL) + `
resource "yet_cloud_application_addon" "files" {
  application_id = "a1"
  kind           = "s3"
  bucket = [
    { environment_id = "e1", name = "acme-assets", existing = true },
  ]
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_application_addon.files", tfjsonpath.New("env_var"), knownvalue.StringExact("S3")),
					statecheck.ExpectKnownValue("yet_cloud_application_addon.files", at.AtMapKey("mode"), knownvalue.StringExact("native")),
					statecheck.ExpectKnownValue("yet_cloud_application_addon.files", at.AtMapKey("identity"), knownvalue.StringExact("app-1-env-1")),
					statecheck.ExpectKnownValue("yet_cloud_application_addon.files", at.AtMapKey("name"), knownvalue.StringExact("yet-acme-api-production-1a2b3c")),
				},
			},
		},
	})
}
