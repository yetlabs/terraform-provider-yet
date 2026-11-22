action "yet_settings_mail_send_email" "launched" {
  config {
    from    = "Acme <ops@${yet_settings_mail_subdomain.notices.name}>"
    to      = ["ana@acme-br.example.com"]
    subject = "The storefront is up"
    html    = "<p>${yet_cloud_application.api.name} runs in production.</p>"
    tags    = { kind = "launch" }

    # Sent once, however many times the action runs.
    idempotency_key = "storefront-launched"
  }
}

# Told by email as soon as the application is made.
resource "terraform_data" "launched" {
  input = yet_cloud_application.api.id

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.yet_settings_mail_send_email.launched]
    }
  }
}

# Or by hand:
#   terraform apply -invoke action.yet_settings_mail_send_email.launched
