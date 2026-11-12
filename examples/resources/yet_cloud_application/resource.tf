resource "yet_cloud_application" "api" {
  namespace_id = yet_cloud_namespace.shop.id
  name         = "api"
  stages       = [yet_cloud_environment.staging.id, yet_cloud_environment.production.id]
  auto_promote = [yet_cloud_environment.staging.id]

  repository = "acme/api"
  branch     = "main"
  replicas   = 2

  # Destroyed, its add-ons go with it — kept, unless said.
  addons_on_delete = "delete"

  # The site at each stage's address; its admin at its own, back-office
  # before it; its metrics inside the cluster only.
  ports = [
    { name = "http", port = 3000, public = true },
    { name = "admin", port = 9000, public = true, address = "back-office" },
    { name = "metrics", port = 9100 },
  ]

  # A copy takes requests once /healthz answers, and is restarted when it
  # stops answering on its admin port.
  readiness = {
    port = "http"
    path = "/healthz"
  }
  liveness = {
    port              = "admin"
    protocol          = "tcp"
    period_seconds    = 20
    failure_threshold = 5
  }

  # Each copy up to half a core and 512 MB.
  cpu_millicores = 500
  memory_mb      = 512

  # Up to six, scaling out past 60% of the processors or 80% of the memory.
  max_replicas = 6
  scale_cpu    = 60
  scale_memory = 80

  env = {
    LOG_LEVEL = "info"
  }
  stage_env = {
    (yet_cloud_environment.production.id) = {
      LOG_LEVEL = "warn"
    }
  }

  # Staging on one copy of 256 MB, never growing; production keeps the
  # size above.
  environment_size = {
    (yet_cloud_environment.staging.id) = {
      replicas     = 1
      max_replicas = 1
      memory_mb    = 256
    }
  }
}

# Two applications of a monorepo, in one namespace: the panel built from its
# own Dockerfile — from the repository's root, so it reaches the schema
# beside its folder — and handed the worker's address inside the cluster.
resource "yet_cloud_application" "worker" {
  namespace_id = yet_cloud_namespace.shop.id
  name         = "worker"
  stages       = [yet_cloud_environment.production.id]
  repository   = "acme/platform"
  branch       = "main"
  build_root   = "worker"
  ports        = [{ name = "http", port = 8091 }]
}

resource "yet_cloud_application" "panel" {
  namespace_id = yet_cloud_namespace.shop.id
  name         = "panel"
  stages       = [yet_cloud_environment.production.id]
  repository   = "acme/platform"
  branch       = "main"
  dockerfile   = "panel/Dockerfile"
  ports        = [{ name = "http", port = 3000, public = true }]
  env = {
    WORKER_URL = yet_cloud_application.worker.internal_url
  }
}
