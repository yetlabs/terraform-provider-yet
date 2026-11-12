resource "yet_cloud_runtime" "mac" {
  name      = "mac-mini"
  stack     = "local:native"
  server_id = yet_cloud_server.mac.id
}

# The organization's macOS runners, registered with the company's GitHub connection.
resource "yet_cloud_runner" "ios" {
  runtime_id = yet_cloud_runtime.mac.id
  name       = "ios"
  url        = "https://github.com/acme"
  labels     = ["macos", "xcode-16"]
  replicas   = 2

  depends_on = [yet_settings_connection_github.acme]
}

# Linux runners on a k3s runtime: each copy sandboxed by gVisor, with a Docker
# of its own for docker run, build and compose — one always ready, up to four
# while jobs wait (the company's GitHub connected as Cloud's App).
resource "yet_cloud_runtime" "builder" {
  name            = "builder"
  stack           = "local:traefik+k3s"
  server_id       = yet_cloud_server.builder.id
  ingress_enabled = false # runners only
}

resource "yet_cloud_runner" "ci" {
  runtime_id   = yet_cloud_runtime.builder.id
  name         = "ci"
  url          = "https://github.com/acme/shop"
  labels       = ["linux-docker"]
  replicas     = 1
  max_replicas = 4

  depends_on = [yet_settings_connection_github.acme]
}
