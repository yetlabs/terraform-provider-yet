resource "yet_cloud_database" "ledger" {
  kind           = "overlite"
  name           = "ledger"
  environment_id = yet_cloud_environment.production.id
}

action "yet_cloud_database_backup" "ledger" {
  config {
    database_id = yet_cloud_database.ledger.id
  }
}

# Run it by hand before a change that matters:
#   terraform apply -invoke action.yet_cloud_database_backup.ledger
