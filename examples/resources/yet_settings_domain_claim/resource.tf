resource "yet_settings_domain_claim" "br" {
  name = "acme.com.br"
}

# Its TXT, wherever the domain's DNS is kept — here, in the company's Cloud.
resource "yet_cloud_dns_record" "proof" {
  zone_id = yet_cloud_dns_zone.br.id
  name    = "@"
  type    = "TXT"
  ttl     = 300
  values  = [yet_settings_domain_claim.br.txt]
}
