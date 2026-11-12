# The sign-in page's logo, from a file next to the configuration.
resource "yet_settings_logo" "acme" {
  source = "${path.module}/logo.svg"
}
