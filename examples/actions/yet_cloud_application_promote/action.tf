action "yet_cloud_application_promote" "api" {
  config {
    application_id      = yet_cloud_application.api.id
    from_environment_id = yet_cloud_environment.staging.id
  }
}

# Run it by hand, when staging looks right:
#   terraform apply -invoke action.yet_cloud_application_promote.api
