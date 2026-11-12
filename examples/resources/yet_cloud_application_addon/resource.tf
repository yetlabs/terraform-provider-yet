# A Postgres of the API's own, on each of its stages.
resource "yet_cloud_application_addon" "db" {
  application_id = yet_cloud_application.api.id
  kind           = "postgres"
  features       = ["pgvector"]
  memory_mb      = 1024
}

# A cache beside each copy of it, on every member of a pool.
resource "yet_cloud_application_addon" "cache" {
  application_id = yet_cloud_application.api.id
  kind           = "redis"
  env_var        = "CACHE_URL"
  placement      = "member"
  features       = ["volatile"]
}

# A bucket on each of its stages, in each stage's AWS account: S3_BUCKET,
# S3_REGION and S3_ENDPOINT, and the way in, handed to it.
resource "yet_cloud_application_addon" "files" {
  application_id = yet_cloud_application.api.id
  kind           = "s3"
}

# Its buckets chosen: staging's a new one, named; production's, one its
# account has already — reached, never made.
resource "yet_cloud_application_addon" "media" {
  application_id = yet_cloud_application.api.id
  kind           = "s3"
  env_var        = "MEDIA"
  bucket = [
    { environment_id = yet_cloud_environment.staging.id, name = "yet-acme-api-media-staging" },
    { environment_id = yet_cloud_environment.production.id, name = "acme-media", existing = true },
  ]
}
