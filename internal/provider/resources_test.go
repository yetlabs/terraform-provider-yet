package provider

import (
	"github.com/hashicorp/terraform-plugin-testing/compare"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// A namespace through its whole life, with Terraform itself: made, renamed,
// imported, destroyed.
func TestNamespaceLifecycle(t *testing.T) {
	yet := newFakeYet(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: providerFor(yet.URL) + `resource "yet_cloud_namespace" "shop" { name = "shop" }`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_namespace.shop", tfjsonpath.New("name"), knownvalue.StringExact("shop")),
					statecheck.ExpectKnownValue("yet_cloud_namespace.shop", tfjsonpath.New("id"), knownvalue.NotNull()),
				},
			},
			{
				Config: providerFor(yet.URL) + `resource "yet_cloud_namespace" "shop" { name = "store" }`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_namespace.shop", tfjsonpath.New("name"), knownvalue.StringExact("store")),
				},
			},
			{
				ResourceName:      "yet_cloud_namespace.shop",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// The company's apps: the ones off, set whole, and in the state as yet has them.
func TestWorkspaceApps(t *testing.T) {
	yet := newFakeYet(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: providerFor(yet.URL) + `resource "yet_settings_apps" "acme" { off = ["sketch", "agile"] }`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_settings_apps.acme", tfjsonpath.New("id"), knownvalue.StringExact("acme")),
					statecheck.ExpectKnownValue("yet_settings_apps.acme", tfjsonpath.New("off"),
						knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("agile"), knownvalue.StringExact("sketch")})),
				},
			},
			{
				Config: providerFor(yet.URL) + `resource "yet_settings_apps" "acme" { off = [] }`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_settings_apps.acme", tfjsonpath.New("off"), knownvalue.SetSizeExact(0)),
				},
			},
		},
	})
}

// Where an application scales out is a step of 5, from 10 to 95 — memory's
// may be 0, never — and Terraform says so before asking yet.
func TestApplicationScaleThresholds(t *testing.T) {
	yet := newFakeYet(t)
	app := func(attrs string) string {
		return providerFor(yet.URL) + `resource "yet_cloud_application" "api" {
  namespace_id = "ns_1"
  name         = "api"
  stages       = ["env_1"]
  ` + attrs + `
}`
	}
	steps := []resource.TestStep{}
	for _, bad := range []string{"scale_cpu = 72", "scale_cpu = 5", "scale_cpu = 0", "scale_memory = 100", "scale_memory = 5"} {
		steps = append(steps, resource.TestStep{Config: app(bad), PlanOnly: true, ExpectError: regexp.MustCompile(`value must be one of`)})
	}
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: factories, Steps: steps})
}

// Against a real company (TF_ACC=1, YET_API_KEY): a cloud and a server,
// made, changed and gone — nothing a company's people would notice.
func TestAccCloudAndServer(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { accPreCheck(t) },
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "yet_cloud_rack" "lab" {
  name        = "Terraform Lab"
  description = "Made by the provider's acceptance tests."
}

resource "yet_cloud_server" "box" {
  name     = "tf-acc-box"
  rack_id = yet_cloud_rack.lab.id
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_server.box", tfjsonpath.New("status"), knownvalue.StringExact("pending")),
					statecheck.ExpectKnownValue("yet_cloud_server.box", tfjsonpath.New("origin"), knownvalue.StringExact("local")),
					statecheck.ExpectKnownValue("yet_cloud_server.box", tfjsonpath.New("join_command"), knownvalue.NotNull()),
				},
			},
			{
				Config: `
resource "yet_cloud_rack" "lab" {
  name = "Terraform Lab 2"
}

resource "yet_cloud_server" "box" {
  name     = "tf-acc-box-2"
  rack_id = yet_cloud_rack.lab.id
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_rack.lab", tfjsonpath.New("description"), knownvalue.StringExact("")),
					statecheck.ExpectKnownValue("yet_cloud_server.box", tfjsonpath.New("name"), knownvalue.StringExact("tf-acc-box-2")),
				},
			},
			{
				ResourceName:            "yet_cloud_server.box",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"join_command", "docker_command"},
			},
		},
	})
}

// Against a real company: Cloud from a server to an application with its
// database, in DNS — the whole of what a stack in code makes.
func TestAccAStack(t *testing.T) {
	config := func(replicas string) string {
		return `
resource "yet_cloud_server" "box" {
  name = "tf-acc-stack"
}

resource "yet_cloud_runtime" "k3s" {
  name      = "tf-acc-stack-k3s"
  stack     = "local:traefik+k3s"
  server_id = yet_cloud_server.box.id
}

resource "yet_cloud_dns_zone" "acme" {
  name = "tf-acc.acme.test"
}

resource "yet_cloud_dns_record" "www" {
  zone_id = yet_cloud_dns_zone.acme.id
  name    = "mail"
  type    = "MX"
  values  = ["10 mx.acme.test."]
}

resource "yet_cloud_environment" "staging" {
  name       = "tf-acc-staging"
  color      = "amber"
  runtime_id = yet_cloud_runtime.k3s.id
}

resource "yet_cloud_namespace" "shop" {
  name = "tf-acc-shop"
}

resource "yet_cloud_application" "api" {
  namespace_id = yet_cloud_namespace.shop.id
  name         = "api"
  stages       = [yet_cloud_environment.staging.id]
  replicas     = ` + replicas + `
  max_replicas = 3
  memory_mb    = 256
  scale_memory = 80
  ports = [
    { name = "http", port = 8080, public = true },
    { name = "admin", port = 9000, public = true },
  ]
  readiness = { port = "http", path = "/healthz" }
  env = {
    LOG_LEVEL = "info"
  }
}

resource "yet_cloud_database" "orders" {
  kind           = "postgres"
  name           = "orders"
  environment_id = yet_cloud_environment.staging.id
}

data "yet_cloud_environment" "staging" {
  name = yet_cloud_environment.staging.name
}

data "yet_cloud_namespace" "shop" {
  name = yet_cloud_namespace.shop.name
}

data "yet_cloud_environment" "by_id" {
  id = yet_cloud_environment.staging.id
}

data "yet_cloud_namespace" "by_id" {
  id = yet_cloud_namespace.shop.id
}

action "yet_cloud_application_release" "api" {
  config {
    application_id = yet_cloud_application.api.id
    image          = "ghcr.io/acme/api:1.0.0"
  }
}

# The first release goes out as the application is made.
resource "terraform_data" "release" {
  input = yet_cloud_application.api.id

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.yet_cloud_application_release.api]
    }
  }
}

data "yet_cloud_runtime" "k3s" {
  name = yet_cloud_runtime.k3s.name
}
`
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { accPreCheck(t) },
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config("1"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_application.api", tfjsonpath.New("port"), knownvalue.Int64Exact(8080)),
					statecheck.ExpectKnownValue("yet_cloud_application.api", tfjsonpath.New("memory_mb"), knownvalue.Int64Exact(256)),
					statecheck.ExpectKnownValue("yet_cloud_application.api", tfjsonpath.New("cpu_millicores"), knownvalue.Int64Exact(0)),
					statecheck.ExpectKnownValue("yet_cloud_application.api", tfjsonpath.New("scale_cpu"), knownvalue.Int64Exact(70)),
					statecheck.ExpectKnownValue("yet_cloud_application.api", tfjsonpath.New("scale_memory"), knownvalue.Int64Exact(80)),
					statecheck.ExpectKnownValue("yet_cloud_application.api", tfjsonpath.New("ports").AtSliceIndex(0).AtMapKey("default"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue("yet_cloud_application.api", tfjsonpath.New("readiness").AtMapKey("period_seconds"), knownvalue.Int64Exact(10)),
					statecheck.ExpectKnownValue("yet_cloud_database.orders", tfjsonpath.New("env_var"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue("data.yet_cloud_environment.staging", tfjsonpath.New("color"), knownvalue.StringExact("amber")),
					statecheck.CompareValuePairs("data.yet_cloud_namespace.shop", tfjsonpath.New("id"), "yet_cloud_namespace.shop", tfjsonpath.New("id"), compare.ValuesSame()),
					statecheck.ExpectKnownValue("data.yet_cloud_namespace.by_id", tfjsonpath.New("name"), knownvalue.StringExact("tf-acc-shop")),
					statecheck.ExpectKnownValue("data.yet_cloud_environment.by_id", tfjsonpath.New("name"), knownvalue.StringExact("tf-acc-staging")),
					statecheck.ExpectKnownValue("yet_cloud_dns_zone.acme", tfjsonpath.New("nameservers"), knownvalue.NotNull()),
				},
			},
			{
				Config: config("2"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_application.api", tfjsonpath.New("replicas"), knownvalue.Int64Exact(2)),
				},
			},
			{ResourceName: "yet_cloud_application.api", ImportState: true, ImportStateVerify: true},
			{ResourceName: "yet_cloud_database.orders", ImportState: true, ImportStateVerify: true},
			{ResourceName: "yet_cloud_environment.staging", ImportState: true, ImportStateVerify: true},
			{ResourceName: "yet_cloud_runtime.k3s", ImportState: true, ImportStateVerify: true},
		},
	})
}

// Against a real company: a pool, its members coming and going.
func TestAccPool(t *testing.T) {
	config := func(members string) string {
		return `
resource "yet_cloud_server" "a" { name = "tf-acc-pool-a" }
resource "yet_cloud_server" "b" { name = "tf-acc-pool-b" }

resource "yet_cloud_runtime" "a" {
  name      = "tf-acc-pool-a"
  stack     = "local:traefik+k3s"
  server_id = yet_cloud_server.a.id
}

resource "yet_cloud_runtime" "b" {
  name      = "tf-acc-pool-b"
  stack     = "local:traefik+k3s"
  server_id = yet_cloud_server.b.id
}

resource "yet_cloud_pool" "web" {
  name        = "tf-acc-web"
  color       = "violet"
  runtime_ids = [` + members + `]
}
`
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { accPreCheck(t) },
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config("yet_cloud_runtime.a.id"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_pool.web", tfjsonpath.New("runtime_ids"), knownvalue.SetSizeExact(1)),
				},
			},
			{
				Config: config("yet_cloud_runtime.a.id, yet_cloud_runtime.b.id"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_pool.web", tfjsonpath.New("runtime_ids"), knownvalue.SetSizeExact(2)),
				},
			},
			{ResourceName: "yet_cloud_pool.web", ImportState: true, ImportStateVerify: true},
		},
	})
}

// Against a real company: a Docker runtime's container, and an environment's
// database attached to an application — the newer resources, end to end.
func TestAccContainersAndAttachments(t *testing.T) {
	config := func(replicas string) string {
		return `
resource "yet_cloud_server" "ci" { name = "tf-acc-ci" }

resource "yet_cloud_runtime" "docker" {
  name      = "tf-acc-ci-docker"
  stack     = "local:docker"
  server_id = yet_cloud_server.ci.id
}

resource "yet_cloud_container" "web" {
  runtime_id = yet_cloud_runtime.docker.id
  name       = "web"
  image      = "nginx:1.29"
  replicas   = ` + replicas + `
  env        = { MODE = "test" }
  secrets    = { TOKEN = "s3cret" }
}

resource "yet_cloud_server" "box" { name = "tf-acc-attach" }

resource "yet_cloud_runtime" "k3s" {
  name      = "tf-acc-attach-k3s"
  stack     = "local:traefik+k3s"
  server_id = yet_cloud_server.box.id
}

resource "yet_cloud_environment" "staging" {
  name       = "tf-acc-attach"
  runtime_id = yet_cloud_runtime.k3s.id
}

resource "yet_cloud_namespace" "shop" { name = "tf-acc-attach-shop" }

resource "yet_cloud_application" "api" {
  namespace_id = yet_cloud_namespace.shop.id
  name         = "api"
  stages       = [yet_cloud_environment.staging.id]
}

resource "yet_cloud_database" "ledger" {
  kind           = "overlite"
  name           = "ledger"
  environment_id = yet_cloud_environment.staging.id
}

resource "yet_cloud_database_attachment" "api_ledger" {
  application_id = yet_cloud_application.api.id
  database_id    = yet_cloud_database.ledger.id
  env_var        = "LEDGER_URL"
}
`
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { accPreCheck(t) },
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config("1"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_container.web", tfjsonpath.New("image"), knownvalue.StringExact("nginx:1.29")),
					statecheck.ExpectKnownValue("yet_cloud_database_attachment.api_ledger", tfjsonpath.New("env_var"), knownvalue.StringExact("LEDGER_URL")),
				},
			},
			{
				Config: config("2"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_container.web", tfjsonpath.New("replicas"), knownvalue.Int64Exact(2)),
				},
			},
			{ResourceName: "yet_cloud_database_attachment.api_ledger", ImportState: true, ImportStateVerify: true},
		},
	})
}
