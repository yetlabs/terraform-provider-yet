resource "yet_cloud_environment" "staging" {
  name       = "staging"
  color      = "amber"
  runtime_id = yet_cloud_runtime.k3s.id
  zone_id    = yet_cloud_dns_zone.acme.id
  prefix     = "staging"
}

# Its applications' S3 buckets made in the production account — and, each
# stage choosing, its applications run as Lambda functions too.
resource "yet_cloud_environment" "production" {
  name           = "production"
  color          = "green"
  pool_id        = yet_cloud_pool.web.id
  zone_id        = yet_cloud_dns_zone.acme.id
  aws_account_id = yet_settings_aws_account.production.id
  functions      = [yet_cloud_runtime.lambda.id]
}

# No servers at all: a serverless server — the AWS account — and a Lambda
# runtime in each region it runs in. The environment may run on both: each
# application's stage picks one (stage_function), by latency or by cost.
resource "yet_cloud_server" "functions" {
  name           = "functions"
  aws_account_id = yet_settings_aws_account.production.id
  service        = "serverless"
}

resource "yet_cloud_runtime" "lambda" {
  name      = "lambda-sa"
  stack     = "aws:lambda"
  server_id = yet_cloud_server.functions.id
  region    = "sa-east-1"
}

resource "yet_cloud_runtime" "lambda_us" {
  name      = "lambda-us"
  stack     = "aws:lambda"
  server_id = yet_cloud_server.functions.id
  region    = "us-east-1"
}

resource "yet_cloud_environment" "functions" {
  name       = "functions"
  runtime_id = yet_cloud_runtime.lambda.id
  functions  = [yet_cloud_runtime.lambda_us.id]
}
