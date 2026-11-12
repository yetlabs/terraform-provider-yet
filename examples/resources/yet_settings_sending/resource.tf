# Through the SES of the company's own AWS account.
resource "yet_settings_sending" "acme" {
  sending = "own"

  depends_on = [yet_settings_connection_aws.acme]
}
