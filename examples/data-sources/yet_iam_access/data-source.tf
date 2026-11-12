data "yet_iam_access" "acme" {}

# Who may change things in production — everywhere, or there.
output "production_developers" {
  value = [
    for g in data.yet_iam_access.acme.grants : g.email
    if g.role != "viewer" && contains(["*", yet_cloud_environment.production.id], g.environment)
  ]
}
