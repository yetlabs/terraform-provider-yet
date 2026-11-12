# Every app on, but these.
resource "yet_settings_apps" "acme" {
  off = ["sketch", "agile"]
}
