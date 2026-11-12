# The role a staging account's stack made, handed to Yet.
resource "yet_settings_aws_account_role" "staging" {
  account_id = yet_settings_aws_account.staging.id
  role_arn   = "arn:aws:iam::123456789013:role/yet-acme-staging-Role-1A2B3C4D5E6F"
}
