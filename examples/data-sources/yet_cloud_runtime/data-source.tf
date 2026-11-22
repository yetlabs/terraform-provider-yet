# By its name…
data "yet_cloud_runtime" "office" {
  name = "office-k3s"
}

# …or by its id.
data "yet_cloud_runtime" "lambda" {
  id = "rt_1"
}
