# Mail to any acme.example.org address that does not exist reaches the list.
resource "yet_settings_mail_catch_all" "acme" {
  domain  = "acme.example.org"
  address = yet_settings_mail_list.finance.address
}
