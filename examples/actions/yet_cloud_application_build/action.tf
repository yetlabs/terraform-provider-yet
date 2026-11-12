action "yet_cloud_application_build" "api" {
  config {
    application_id = yet_cloud_application.api.id
  }
}

# Built from its branch as soon as the application is made, not at the next push.
resource "terraform_data" "first_build" {
  input = yet_cloud_application.api.id

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.yet_cloud_application_build.api]
    }
  }
}

# Or by hand:
#   terraform apply -invoke action.yet_cloud_application_build.api
