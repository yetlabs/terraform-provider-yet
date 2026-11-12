action "yet_cloud_application_pod_delete" "api" {
  config {
    application_id = yet_cloud_application.api.id
    pod            = "api-7d9f8-x2k4p"
  }
}

# Run it by hand, when one copy misbehaves; a fresh one takes its place:
#   terraform apply -invoke action.yet_cloud_application_pod_delete.api
