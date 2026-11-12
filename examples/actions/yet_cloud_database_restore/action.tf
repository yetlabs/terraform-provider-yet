action "yet_cloud_database_restore" "ledger" {
  config {
    database_id = yet_cloud_database.ledger.id
    backup_id   = "bk_1"
  }
}

# Run it by hand, once the copy is the one — looked into in its Studio first:
#   terraform apply -invoke action.yet_cloud_database_restore.ledger
