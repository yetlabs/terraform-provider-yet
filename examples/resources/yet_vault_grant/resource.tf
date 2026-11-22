# The api reads the vault's secrets: AUTH_SECRET, in its variables.
resource "yet_vault_grant" "api" {
  vault_id       = yet_vault_vault.production.id
  application_id = yet_cloud_application.api.id
}

resource "yet_cloud_application" "api" {
  namespace_id = yet_cloud_namespace.shop.id
  name         = "api"
  stages       = [yet_cloud_environment.production.id]
  secret_env = {
    AUTH_SECRET = yet_vault_secret.auth.id
  }
}
