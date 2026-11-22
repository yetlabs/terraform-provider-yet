variable "auth_secret" {
  type      = string
  sensitive = true
}

resource "yet_vault_secret" "auth" {
  vault_id = yet_vault_vault.production.id
  name     = "AUTH_SECRET"
  note     = "Signs the sessions of the api."
  value    = var.auth_secret
}
