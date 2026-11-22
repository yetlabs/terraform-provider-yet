# Order notices from a subdomain of their own, apart from the people's mail.
resource "yet_settings_mail_subdomain" "notices" {
  name = "avisos.acme-br.example.com"

  depends_on = [yet_settings_domain.br]
}
