data "yet_cloud_stacks" "all" {}

# The stacks a runtime can be made on now.
output "available_stacks" {
  value = [for s in data.yet_cloud_stacks.all.stacks : s.id if s.available]
}
