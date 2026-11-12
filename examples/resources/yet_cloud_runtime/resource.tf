resource "yet_cloud_runtime" "k3s" {
  name      = "box-k3s"
  stack     = "local:traefik+k3s"
  server_id = yet_cloud_server.box.id
}

# One for GitHub's runners only: its ingress off, so it takes no environment.
resource "yet_cloud_runtime" "ci" {
  name            = "box-ci"
  stack           = "local:traefik+k3s"
  server_id       = yet_cloud_server.box.id
  ingress_enabled = false
}
