# By its name…
data "yet_cloud_environment" "production" {
  name = "production"
}

# …or by its id.
data "yet_cloud_environment" "staging" {
  id = "env_1"
}
