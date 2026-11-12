data "yet_settings_github_repositories" "acme" {}

# An application for each of the company's service-… repositories.
resource "yet_cloud_application" "service" {
  for_each = {
    for r in data.yet_settings_github_repositories.acme.repositories : r.full_name => r
    if startswith(r.full_name, "acme/service-")
  }

  namespace_id = yet_cloud_namespace.services.id
  name         = trimprefix(each.key, "acme/service-")
  stages       = [yet_cloud_environment.staging.id]
  repository   = each.key
  branch       = each.value.default_branch
}
