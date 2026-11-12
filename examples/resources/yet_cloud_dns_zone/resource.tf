resource "yet_cloud_dns_zone" "acme" {
  name = "acme.dev"
}

# Where the registrar has to point.
output "nameservers" {
  value = yet_cloud_dns_zone.acme.nameservers
}
