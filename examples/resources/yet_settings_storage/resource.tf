# Drive's files in the company's own bucket, under yet/drive.
resource "yet_settings_storage" "acme" {
  bucket = "acme-drive"
  prefix = "yet/drive"

  depends_on = [yet_settings_connection_aws.acme]
}
