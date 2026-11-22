resource "yet_settings_mail_list" "finance" {
  domain      = "acme.example.org"
  name        = "financeiro"
  description = "Invoices and receipts."
  recipients = [
    yet_settings_person.ana.email,
    "accounting@example.com",
  ]
}
