resource "yet_cloud_database" "orders" {
  kind           = "postgres"
  name           = "orders"
  environment_id = yet_cloud_environment.production.id
  features       = ["pgvector"]
  memory_mb      = 1024
}

# An Overlite on a runtime of the company's, copied every 6 hours, the last 14 kept.
resource "yet_cloud_database" "ledger" {
  kind           = "overlite"
  name           = "ledger"
  environment_id = yet_cloud_environment.production.id
  backup_every   = 6
  backup_keep    = 14
}
