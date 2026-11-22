# Vault's one bucket is made by the stack of the account connected for it.
resource "yet_settings_aws_account" "secrets" {
  name   = "Secrets"
  region = "sa-east-1"
  uses   = ["vault"]
}

# What the company's applications read: production's, for systems.
resource "yet_vault_vault" "production" {
  name        = "Production"
  description = "What our applications sign in with."
}

# The engineers', shared: who reads it is given in Yet's apps.
resource "yet_vault_vault" "engineering" {
  name = "Engineering"
  kind = "shared"
}
