variable "github_token" {
  type      = string
  sensitive = true
}

# What the company's runners register with, when they have no token of their own.
resource "yet_settings_connection_github" "acme" {
  token = var.github_token
}
