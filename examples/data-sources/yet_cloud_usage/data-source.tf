data "yet_cloud_usage" "now" {}

# The quotas past 70%: time to ask AWS for more.
output "nearly_used" {
  value = [
    for l in data.yet_cloud_usage.now.limits : "${l.account} ${l.region} ${l.name}: ${l.used} of ${l.max}"
    if l.max > 0 && l.used / l.max >= 0.7
  ]
}
