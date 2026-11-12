# The orders the shop sends, and their answers, read by the people on them.
resource "yet_settings_mail_shared_mailbox" "orders" {
  address = "pedidos@${yet_settings_mail_subdomain.notices.name}"
  name    = "Pedidos"
  members = [
    yet_settings_person.ana.email,
  ]
}
