resource "yet_cloud_database" "ledger" {
  kind           = "overlite"
  name           = "ledger"
  environment_id = yet_cloud_environment.production.id
}

# The production ledger, handed to the API on its production stage.
resource "yet_cloud_database_attachment" "api_ledger" {
  application_id = yet_cloud_application.api.id
  database_id    = yet_cloud_database.ledger.id
  env_var        = "LEDGER_URL"
}
