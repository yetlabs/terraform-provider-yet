data "yet_iam_api_keys" "acme" {}

# Keys never used — candidates to revoke, in Settings → IAM.
output "unused_keys" {
  value = [for k in data.yet_iam_api_keys.acme.keys : "${k.email}: ${k.name}" if k.last_used_at == ""]
}
