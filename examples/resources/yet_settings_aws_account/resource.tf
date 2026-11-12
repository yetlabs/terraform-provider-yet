# The production account, for Mail's SES, Drive's documents and Cloud's copies:
# added to Yet, its stack made with the AWS provider, its role handed back.
data "yet_settings_aws_launch" "yet" {}

resource "yet_settings_aws_account" "production" {
  name   = "Production"
  region = "us-east-1"
  uses   = ["mail", "drive", "databases"]
}

resource "aws_cloudformation_stack" "yet" {
  name         = "yet-acme-production"
  template_url = data.yet_settings_aws_launch.yet.template_url
  capabilities = ["CAPABILITY_IAM"]
  parameters = {
    YetPrincipal = data.yet_settings_aws_launch.yet.principal
    ExternalId   = yet_settings_aws_account.production.external_id
    Company      = "acme"
    UseMail      = "true"
    UseDrive     = "true"
    UseDatabases = "true"
  }
}

resource "yet_settings_aws_account_role" "production" {
  account_id = yet_settings_aws_account.production.id
  role_arn   = aws_cloudformation_stack.yet.outputs["RoleArn"]
}
