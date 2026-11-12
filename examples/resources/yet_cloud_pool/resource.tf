# Two runtimes, on two clouds, running the same environments — behind one tunnel.
resource "yet_cloud_pool" "web" {
  name        = "web"
  color       = "violet"
  runtime_ids = [yet_cloud_runtime.office.id, yet_cloud_runtime.aws.id]
  tunnel      = true
}
