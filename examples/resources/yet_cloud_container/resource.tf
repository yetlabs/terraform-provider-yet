resource "yet_cloud_runtime" "ci" {
  name      = "box-docker"
  stack     = "local:docker"
  server_id = yet_cloud_server.box.id
}

# Docker in Docker, for CI runners to build in.
resource "yet_cloud_container" "dind" {
  runtime_id = yet_cloud_runtime.ci.id
  name       = "dind"
  image      = "docker:27-dind"
  privileged = true
  volumes    = ["dind-cache:/var/lib/docker"]

  env = {
    DOCKER_TLS_CERTDIR = ""
  }
}

variable "registry_token" {
  type      = string
  sensitive = true
}

# Three runners alike, each taking a job.
resource "yet_cloud_container" "runner" {
  runtime_id = yet_cloud_runtime.ci.id
  name       = "runner"
  image      = "ghcr.io/acme/ci-runner:2"
  replicas   = 3

  env = {
    DOCKER_HOST = "tcp://dind:2375"
  }
  secrets = {
    REGISTRY_TOKEN = var.registry_token
  }
}
