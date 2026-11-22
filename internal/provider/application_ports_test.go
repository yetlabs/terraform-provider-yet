package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"sync"
	"testing"

	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	yet "github.com/yetlabs/yet-sdk-go"
)

/*
fakeApps is Cloud's applications as far as their ports and checks go: one
application, its ports' default picked and its checks' left-outs filled as
yet does, a check absent kept and null removed.
*/
type fakeApps struct {
	mu  sync.Mutex
	app *yet.Application
}

func newFakeApps(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(&fakeApps{})
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeApps) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var in struct {
		yet.ApplicationInput
		Readiness json.RawMessage `json:"readiness"`
		Liveness  json.RawMessage `json:"liveness"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case r.Method == "POST" && r.URL.Path == "/v1/cloud/applications":
		f.app = &yet.Application{ID: "app1", Port: 8080, Replicas: 1, ScaleCPU: 70, Env: map[string]string{}}
		f.apply(in.ApplicationInput, in.Readiness, in.Liveness)
		reply(201, f.app)
	case f.app == nil || r.URL.Path != "/v1/cloud/applications/app1":
		reply(404, map[string]string{"error": "No such thing."})
	case r.Method == "GET":
		reply(200, f.app)
	case r.Method == "PATCH":
		f.apply(in.ApplicationInput, in.Readiness, in.Liveness)
		reply(200, f.app)
	case r.Method == "DELETE":
		f.app = nil
		w.WriteHeader(http.StatusNoContent)
	}
}

func (f *fakeApps) apply(in yet.ApplicationInput, readiness, liveness json.RawMessage) {
	a := f.app
	if in.NamespaceID != nil {
		a.NamespaceID, a.Name, a.Stages = *in.NamespaceID, *in.Name, *in.Stages
	}
	if in.Port != nil {
		a.Port = *in.Port
		for i := range a.Ports {
			if a.Ports[i].Default {
				a.Ports[i].Port = *in.Port
			}
		}
	}
	hadYaP := slices.ContainsFunc(a.Ports, func(p yet.AppPort) bool { return p.YaP })
	if in.Ports != nil {
		a.Ports = *in.Ports
		chosen := -1
		for i := range a.Ports {
			if a.Ports[i].Protocol == "" {
				a.Ports[i].Protocol = "http"
			}
			if a.Ports[i].Default {
				chosen = i
			}
		}
		for i := range a.Ports {
			if chosen < 0 && a.Ports[i].Public {
				chosen = i
			}
		}
		for i := range a.Ports {
			a.Ports[i].Default = i == chosen
			if a.Ports[i].Default || !a.Ports[i].Public {
				a.Ports[i].Address = ""
			}
		}
		a.Port = a.Ports[max(chosen, 0)].Port
	}
	if in.YaPEveryone != nil {
		a.YaPEveryone = *in.YaPEveryone
	}
	if in.YaPAllowed != nil {
		a.YaPAllowed = *in.YaPAllowed
	}
	// Its first YaP port lets everyone in, unless said.
	if !hadYaP && slices.ContainsFunc(a.Ports, func(p yet.AppPort) bool { return p.YaP }) && in.YaPEveryone == nil && len(a.YaPAllowed) == 0 {
		a.YaPEveryone = true
	}
	if in.Replicas != nil {
		a.Replicas = *in.Replicas
	}
	if in.EnvironmentSize != nil {
		a.EnvironmentSize = map[string]yet.EnvironmentSize{}
		for env, st := range *in.EnvironmentSize {
			if st != (yet.EnvironmentSize{}) {
				a.EnvironmentSize[env] = st
			}
		}
	}
	if a.Ports == nil {
		a.Ports = []yet.AppPort{{Name: "http", Port: a.Port, Protocol: "http", Public: true, Default: true}}
	}
	for _, x := range []struct {
		raw json.RawMessage
		to  **yet.Probe
	}{{readiness, &a.Readiness}, {liveness, &a.Liveness}} {
		if len(x.raw) == 0 {
			continue
		}
		var p *yet.Probe
		_ = json.Unmarshal(x.raw, &p)
		if p != nil {
			p.Protocol = cmpOr(p.Protocol, "http")
			p.PeriodSeconds, p.TimeoutSeconds = max(p.PeriodSeconds, 10), max(p.TimeoutSeconds, 1)
			p.FailureThreshold = max(p.FailureThreshold, 3)
		}
		*x.to = p
	}
}

func cmpOr(s, or string) string {
	if s == "" {
		return or
	}
	return s
}

// An application's ports and checks through a life: the default picked and
// port its number; a check set, then removed; port alone moving the
// default's; imported as it is.
func TestApplicationPortsAndChecks(t *testing.T) {
	srv := newFakeApps(t)
	app := func(rest string) string {
		return cloudProvider(srv.URL) + `
resource "yet_cloud_application" "api" {
  namespace_id = "ns1"
  name         = "api"
  stages       = ["env1"]
` + rest + `
}
`
	}
	at := func(attr string) tfjsonpath.Path { return tfjsonpath.New(attr) }
	is := func(p tfjsonpath.Path, v knownvalue.Check) statecheck.StateCheck {
		return statecheck.ExpectKnownValue("yet_cloud_application.api", p, v)
	}
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []tfresource.TestStep{
			{
				Config: app(""),
				ConfigStateChecks: []statecheck.StateCheck{
					is(at("port"), knownvalue.Int64Exact(8080)),
					is(at("ports").AtSliceIndex(0).AtMapKey("name"), knownvalue.StringExact("http")),
					is(at("readiness"), knownvalue.Null()),
				},
			},
			{
				Config: app(`
  ports = [
    { name = "http", port = 3000, public = true },
    { name = "admin", port = 9000, public = true, address = "back-office" },
    { name = "metrics", port = 9100 },
  ]
  readiness = { port = "http", path = "/healthz" }
  liveness  = { port = "metrics", protocol = "tcp", period_seconds = 20 }
`),
				ConfigStateChecks: []statecheck.StateCheck{
					is(at("port"), knownvalue.Int64Exact(3000)),
					is(at("ports").AtSliceIndex(0).AtMapKey("default"), knownvalue.Bool(true)),
					is(at("ports").AtSliceIndex(0).AtMapKey("address"), knownvalue.Null()),
					is(at("ports").AtSliceIndex(1).AtMapKey("default"), knownvalue.Bool(false)),
					is(at("ports").AtSliceIndex(1).AtMapKey("address"), knownvalue.StringExact("back-office")),
					is(at("ports").AtSliceIndex(2).AtMapKey("protocol"), knownvalue.StringExact("http")),
					is(at("readiness").AtMapKey("period_seconds"), knownvalue.Int64Exact(10)),
					is(at("liveness").AtMapKey("path"), knownvalue.Null()),
				},
			},
			{
				Config: app(`
  ports = [
    { name = "http", port = 3000, public = true },
    { name = "admin", port = 9000, public = true, default = true },
  ]
  readiness = { port = "http", path = "/healthz" }
`),
				ConfigStateChecks: []statecheck.StateCheck{
					is(at("port"), knownvalue.Int64Exact(9000)),
					is(at("ports").AtSliceIndex(0).AtMapKey("default"), knownvalue.Bool(false)),
					is(at("liveness"), knownvalue.Null()),
				},
			},
			{
				Config: app(`  port = 9001`),
				ConfigStateChecks: []statecheck.StateCheck{
					is(at("ports").AtSliceIndex(1).AtMapKey("port"), knownvalue.Int64Exact(9001)),
					is(at("readiness"), knownvalue.Null()),
				},
			},
			{ResourceName: "yet_cloud_application.api", ImportState: true, ImportStateVerify: true},
		},
	})
}

// YaP: a public http port the company's people get through signed in —
// everyone at first, then the ones said — and never a private one.
func TestApplicationYaP(t *testing.T) {
	srv := newFakeApps(t)
	app := func(rest string) string {
		return cloudProvider(srv.URL) + `
resource "yet_cloud_application" "admin" {
  namespace_id = "ns1"
  name         = "admin"
  stages       = ["env1"]
` + rest + `
}
`
	}
	is := func(p tfjsonpath.Path, v knownvalue.Check) statecheck.StateCheck {
		return statecheck.ExpectKnownValue("yet_cloud_application.admin", p, v)
	}
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []tfresource.TestStep{
			{
				Config:      app(`  ports = [{ name = "http", port = 3000, yap = true }]`),
				ExpectError: regexp.MustCompile(`A YaP port is public`),
			},
			{
				Config: app(`  ports = [{ name = "http", port = 3000, public = true, yap = true }]`),
				ConfigStateChecks: []statecheck.StateCheck{
					is(tfjsonpath.New("ports").AtSliceIndex(0).AtMapKey("yap"), knownvalue.Bool(true)),
					is(tfjsonpath.New("yap_everyone"), knownvalue.Bool(true)),
				},
			},
			{
				Config: app(`
  ports        = [{ name = "http", port = 3000, public = true, yap = true }]
  yap_everyone = false
  yap_allowed  = ["ops@acme.dev"]
`),
				ConfigStateChecks: []statecheck.StateCheck{
					is(tfjsonpath.New("yap_everyone"), knownvalue.Bool(false)),
					is(tfjsonpath.New("yap_allowed"), knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact("ops@acme.dev")})),
				},
			},
			{ResourceName: "yet_cloud_application.admin", ImportState: true, ImportStateVerify: true},
		},
	})
}

// An environment's own size: what it says kept, what it leaves out null — the
// application's — and an environment with nothing of its own dropped.
func TestApplicationEnvironmentSize(t *testing.T) {
	srv := newFakeApps(t)
	app := func(environmentSize string) string {
		return cloudProvider(srv.URL) + `
resource "yet_cloud_application" "api" {
  namespace_id = "ns1"
  name         = "api"
  stages       = ["env1", "env2"]
  replicas     = 3
  environment_size   = ` + environmentSize + `
}
`
	}
	size := func(env, field string) tfjsonpath.Path {
		return tfjsonpath.New("environment_size").AtMapKey(env).AtMapKey(field)
	}
	is := func(p tfjsonpath.Path, v knownvalue.Check) statecheck.StateCheck {
		return statecheck.ExpectKnownValue("yet_cloud_application.api", p, v)
	}
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []tfresource.TestStep{
			{Config: app(`{ env1 = { scale_cpu = 12 } }`), PlanOnly: true, ExpectError: regexp.MustCompile(`value must be one of`)},
			{
				Config: app(`{ env1 = { replicas = 1, max_replicas = 1, memory_mb = 512 } }`),
				ConfigStateChecks: []statecheck.StateCheck{
					is(size("env1", "replicas"), knownvalue.Int64Exact(1)),
					is(size("env1", "memory_mb"), knownvalue.Int64Exact(512)),
					is(size("env1", "cpu_millicores"), knownvalue.Null()),
					is(tfjsonpath.New("replicas"), knownvalue.Int64Exact(3)),
				},
			},
			{
				Config: app(`{ env2 = { scale_memory = 80 } }`),
				ConfigStateChecks: []statecheck.StateCheck{
					is(tfjsonpath.New("environment_size"), knownvalue.MapSizeExact(1)),
					is(size("env2", "scale_memory"), knownvalue.Int64Exact(80)),
					is(size("env2", "replicas"), knownvalue.Null()),
				},
			},
			{ResourceName: "yet_cloud_application.api", ImportState: true, ImportStateVerify: true},
		},
	})
}

// Ports and checks yet would refuse, Terraform refuses before asking it.
func TestApplicationPortsAndChecksHold(t *testing.T) {
	srv := newFakeApps(t)
	app := func(attrs string) string {
		return cloudProvider(srv.URL) + `resource "yet_cloud_application" "api" {
  namespace_id = "ns1"
  name         = "api"
  stages       = ["env1"]
  ` + attrs + `
}`
	}
	steps := []tfresource.TestStep{}
	for bad, says := range map[string]string{
		`ports = []`:                                                                                                                  `at least 1`,
		`ports = [{ name = "Web", port = 80 }]`:                                                                                       `lowercase letters`,
		`ports = [{ name = "web", port = 70000 }]`:                                                                                    `between 1 and 65535`,
		`ports = [{ name = "web", port = 80, protocol = "udp" }]`:                                                                     `value must be one of`,
		`ports = [{ name = "web", port = 80 }, { name = "web", port = 81 }]`:                                                          `Two ports are named so`,
		`ports = [{ name = "a", port = 80 }, { name = "b", port = 80 }]`:                                                              `Two ports listen on it`,
		`ports = [{ name = "db", port = 5432, protocol = "tcp", public = true }]`:                                                     `A TCP port is not public`,
		`ports = [{ name = "web", port = 80, default = true }]`:                                                                       `The default port is public`,
		`port = 80` + "\n" + `ports = [{ name = "web", port = 80 }]`:                                                                  `cannot be specified when`,
		`ports = [{ name = "web", port = 80, public = true, address = "Back" }]`:                                                      `subdomain label`,
		`ports = [{ name = "web", port = 80, address = "back" }]`:                                                                     `Only a public port has an address`,
		`ports = [{ name = "web", port = 80, public = true, address = "back" }]`:                                                      `The default port has no address`,
		`ports = [{ name = "a", port = 80, public = true }, { name = "b", port = 81, public = true, default = true, address = "x" }]`: `The default port has no address`,
		`ports = [{ name = "a", port = 80, public = true }, { name = "b", port = 81, public = true, address = "x" }, { name = "c", port = 82, public = true, address = "x" }]`: `Two ports answer there`,
		`ports = [{ name = "web", port = 80 }]` + "\n" + `readiness = { port = "x", path = "/" }`:                                                                              `No such port`,
		`readiness = { port = "http" }`:                                                     `An HTTP check has a path`,
		`readiness = { port = "http", protocol = "tcp", path = "/" }`:                       `Only an HTTP check has a path`,
		`readiness = { port = "http", path = "healthz" }`:                                   `must start with /`,
		`liveness = { port = "http", path = "/", period_seconds = 5, timeout_seconds = 6 }`: `It waits too long`,
		`liveness = { port = "http", path = "/", failure_threshold = 31 }`:                  `between 1 and 30`,
	} {
		steps = append(steps, tfresource.TestStep{Config: app(bad), PlanOnly: true, ExpectError: regexp.MustCompile(says)})
	}
	tfresource.UnitTest(t, tfresource.TestCase{ProtoV6ProviderFactories: factories, Steps: steps})
}
