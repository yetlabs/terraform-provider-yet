resource "yet_cloud_dns_zone" "acme" {
  name = "acme.dev"
}

action "yet_cloud_dns_zone_verify" "acme" {
  config {
    zone_id = yet_cloud_dns_zone.acme.id
  }
}

# Once the registrar points at the zone's nameservers:
#   terraform apply -invoke action.yet_cloud_dns_zone_verify.acme
