# By its domain…
data "yet_cloud_dns_zone" "acme" {
  name = "acme.dev"
}

# …or by its id.
data "yet_cloud_dns_zone" "shop" {
  id = "zn_1"
}
