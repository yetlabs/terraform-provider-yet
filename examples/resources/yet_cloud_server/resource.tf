resource "yet_cloud_server" "box" {
  name    = "box"
  rack_id = yet_cloud_rack.home.id
}

# A VM that joins Cloud as it boots.
resource "aws_instance" "box" {
  ami           = "ami-0abcdef1234567890"
  instance_type = "t4g.medium"
  user_data     = "#!/bin/sh\n${yet_cloud_server.box.join_command}"
}

# An EC2 instance Cloud starts in an AWS account connected for Servers: it
# joins by itself as it boots; destroying it ends the instance.
resource "yet_cloud_server" "ec2" {
  name           = "ec2-1"
  rack_id        = yet_cloud_rack.home.id
  aws_account_id = yet_settings_aws_account.production.id
  instance_type  = "t4g.medium"
  disk_gb        = 50
}
