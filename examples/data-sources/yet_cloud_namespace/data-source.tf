# By its name…
data "yet_cloud_namespace" "shop" {
  name = "Shop"
}

# …or by its id.
data "yet_cloud_namespace" "billing" {
  id = "ns_1"
}
