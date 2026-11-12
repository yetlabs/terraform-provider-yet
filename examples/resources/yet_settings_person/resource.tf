resource "yet_settings_person" "ana" {
  name      = "ana"
  domain    = "acme.dev"
  aliases   = ["acme.com.br"]
  full_name = "Ana Costa"
  password  = var.ana_password
}

variable "ana_password" {
  type      = string
  sensitive = true
}
