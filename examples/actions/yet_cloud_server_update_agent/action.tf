resource "yet_cloud_server" "nuc" {
  name = "nuc"
}

action "yet_cloud_server_update_agent" "nuc" {
  config {
    server_id = yet_cloud_server.nuc.id
  }
}

# Its agent behind Cloud, after a deploy:
#   terraform apply -invoke action.yet_cloud_server_update_agent.nuc
