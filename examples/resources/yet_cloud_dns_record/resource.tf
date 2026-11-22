resource "yet_cloud_dns_record" "mx" {
  zone_id = yet_cloud_dns_zone.acme.id
  name    = "@"
  type    = "MX"
  ttl     = 3600
  values  = ["10 mx1.acme.example.org", "20 mx2.acme.example.org"]
}
