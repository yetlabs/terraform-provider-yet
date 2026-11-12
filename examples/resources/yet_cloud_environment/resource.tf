resource "yet_cloud_environment" "staging" {
  name       = "staging"
  color      = "amber"
  runtime_id = yet_cloud_runtime.k3s.id
  zone_id    = yet_cloud_dns_zone.acme.id
  prefix     = "staging"
}

# Its applications' S3 buckets made in the production account.
resource "yet_cloud_environment" "production" {
  name           = "production"
  color          = "green"
  pool_id        = yet_cloud_pool.web.id
  zone_id        = yet_cloud_dns_zone.acme.id
  aws_account_id = yet_settings_aws_account.production.id
}
