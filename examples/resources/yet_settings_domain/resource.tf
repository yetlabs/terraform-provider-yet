# The company's once its TXT answers; then its mail, here.
resource "yet_settings_domain" "br" {
  name = yet_settings_domain_claim.br.name
  mail = true

  depends_on = [yet_cloud_dns_record.proof]
}
