data "yet_settings_github_branches" "storefront" {
  repository = "acme/storefront"
}

# Built from its release branch when it has one, from main otherwise.
resource "yet_cloud_application" "storefront" {
  namespace_id = yet_cloud_namespace.shop.id
  name         = "storefront"
  stages       = [yet_cloud_environment.production.id]
  repository   = data.yet_settings_github_branches.storefront.repository
  branch       = contains(data.yet_settings_github_branches.storefront.branches, "release") ? "release" : "main"
}
