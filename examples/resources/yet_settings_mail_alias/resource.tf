# contato@acme.example.org, landing in Ana's inbox.
resource "yet_settings_mail_alias" "contact" {
  person_id = yet_settings_person.ana.id
  name      = "contato"
}
