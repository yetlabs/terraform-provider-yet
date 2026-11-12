# The storefront's Postgres, kept when the storefront went, adopted by the API.
action "yet_cloud_database_adopt" "orders" {
  config {
    database_id    = "01J9Z8K3V7Q2M4N6P8R0S2T4V6"
    application_id = yet_cloud_application.api.id
  }
}

# Run it by hand:
#   terraform apply -invoke action.yet_cloud_database_adopt.orders
