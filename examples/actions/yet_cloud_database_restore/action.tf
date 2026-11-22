action "yet_cloud_database_restore" "ledger" {
  config {
    database_id = yet_cloud_database.ledger.id
    backup_id   = "bk_1"
  }
}

# Run it by hand, once the copy is the one — looked into in its Studio first:
#   terraform apply -invoke action.yet_cloud_database_restore.ledger

# A BucketDB's version, from its history — each commit its bucket keeps:
action "yet_cloud_database_restore" "orders" {
  config {
    database_id = yet_cloud_database.orders.id
    at          = "3/L4kqtJlcpXroDTDmJ+rmSpXd3dIbrHY"
  }
}
