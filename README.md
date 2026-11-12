# Terraform provider for Yet

Every product of a [Yet](https://yet.com.br) company as code — its Settings
and its Cloud today, the rest as they open to the API. Built on
[yet-sdk-go](../yet-sdk-go), with HashiCorp's
[plugin framework](https://developer.hashicorp.com/terraform/plugin/framework).

```hcl
terraform {
  required_providers {
    yet = {
      source = "yetlabs/yet"
    }
  }
}

provider "yet" {} # the key from YET_API_KEY

resource "yet_cloud_rack" "home" {
  name = "Home Rack"
  kind = "local"
}

resource "yet_cloud_server" "box" {
  name    = "box"
  rack_id = yet_cloud_rack.home.id
}

resource "yet_cloud_runtime" "k3s" {
  name      = "box-k3s"
  stack     = "local:traefik+k3s"
  server_id = yet_cloud_server.box.id
}

resource "yet_cloud_environment" "staging" {
  name       = "staging"
  runtime_id = yet_cloud_runtime.k3s.id
}

resource "yet_cloud_database" "orders" {
  kind           = "postgres"
  name           = "orders"
  environment_id = yet_cloud_environment.staging.id
}
```

The [docs](docs) list every resource and data source; they are generated from
the schemas and [examples](examples) — `make generate`, never by hand.

## What it covers

| product  | resources                                                                                                                                                                                                                | data sources                                                       |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------ |
| Settings | `yet_settings_workspace`, `yet_settings_apps`, `yet_settings_person`, `yet_settings_mail_subdomain`, `yet_settings_mail_shared_mailbox`, `yet_settings_sso_app`                                                          |                                                                    |
| Cloud    | `yet_cloud_rack`, `yet_cloud_server`, `yet_cloud_runtime`, `yet_cloud_pool`, `yet_cloud_environment`, `yet_cloud_namespace`, `yet_cloud_application`, `yet_cloud_database`, `yet_cloud_dns_zone`, `yet_cloud_dns_record` | `yet_cloud_environment`, `yet_cloud_runtime`, `yet_cloud_dns_zone` |

Whatever Yet's apps can change has its analog here — the rule is in the
repository's `AGENTS.md`.

## Authentication

An API key, made in Yet in **Preferences → API keys**: `yet_<company>_<secret>`. It
names the company and acts as the person it was made for, in every product —
the provider does what they may do, nothing more. Settings take a key of
someone who runs them.

| argument      | environment       | default                |
| ------------- | ----------------- | ---------------------- |
| `api_key`     | `YET_API_KEY`     | —                      |
| `base_domain` | `YET_BASE_DOMAIN` | `yet.com.br`           |
| `cloud_url`   | `YET_CLOUD_URL`   | `https://cloud.<base>` |

Every product is at `cloud_url`, yet's API (`/v1/<product>`).

## Development

```sh
make build      # go build
make generate   # the docs, from the schemas and examples/
make test       # unit tests: Terraform itself against a yet kept in memory
make testacc    # acceptance tests, against a real company (see below)
```

`make test` needs Terraform installed (or downloads it). The acceptance tests
make and remove things in the company the key is of: run them against a yet
on a laptop (`YET_CLOUD_URL=http://localhost:8096`,
`YET_WORKSPACE_URL=http://acme.localhost:3001`), or a company kept for tests.

To try a build, point Terraform at it in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "yetlabs/yet" = "/Users/you/go/bin"
  }
  direct {}
}
```

then `make install`, and `terraform plan` uses it — no `terraform init`.

## Releases

This module is written in Yet's repository, beside the SDK (tied by the
root `go.work`), and published at `github.com/yetlabs/terraform-provider-yet`:
each push to Yet's main puts this folder there, its `go.mod` requiring the
SDK (`github.com/yetlabs/yet-sdk-go`) by version. A release —
release-please's `terraform-provider-yet-v1.2.0` in Yet's repository — is
tagged `v1.2.0` in yetlabs', and
`.goreleaser.yml` builds it, signed with the Registry's GPG key
(`GPG_PRIVATE_KEY`, `PASSPHRASE`), for the Terraform Registry. Its files are
also at Yet's own addresses —
`https://releases.yet.com.br/terraform-provider-yet/<version>/…` — which
outlive where they are kept.
