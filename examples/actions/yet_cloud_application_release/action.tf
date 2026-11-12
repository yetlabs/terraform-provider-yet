variable "api_image" {
  type = string
}

action "yet_cloud_application_release" "api" {
  config {
    application_id = yet_cloud_application.api.id
    image          = var.api_image
  }
}

# A new image released whenever the variable changes:
#   terraform apply -var api_image=ghcr.io/acme/api:1.4.0
resource "terraform_data" "api_image" {
  input = var.api_image

  lifecycle {
    action_trigger {
      events  = [after_create, after_update]
      actions = [action.yet_cloud_application_release.api]
    }
  }
}
