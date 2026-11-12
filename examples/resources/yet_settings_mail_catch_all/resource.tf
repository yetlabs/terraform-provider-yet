# Mail to any acme.dev address that does not exist reaches the list.
resource "yet_settings_mail_catch_all" "acme" {
  domain  = "acme.dev"
  address = yet_settings_mail_list.finance.address
}
