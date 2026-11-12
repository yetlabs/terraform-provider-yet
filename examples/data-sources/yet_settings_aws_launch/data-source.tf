data "yet_settings_aws_launch" "yet" {}

output "yet_template" {
  value = data.yet_settings_aws_launch.yet.template_url
}
