# The company's CRM signs its sales team in with Yet, and has a tile in
# their apps menu. Its client goes to the CRM's configuration.
resource "yet_settings_sso_app" "crm" {
  name          = "CRM"
  redirect_uris = ["https://crm.acme.com.br/auth/callback"]
  allowed = [
    yet_settings_person.ana.email,
    "vendas@acme.com.br",
  ]
  in_menu     = true
  home_url    = "https://crm.acme.com.br"
  privacy_url = "https://crm.acme.com.br/privacy"
  terms_url   = "https://crm.acme.com.br/terms"
}

output "crm_client_id" {
  value = yet_settings_sso_app.crm.client_id
}

output "crm_client_secret" {
  value     = yet_settings_sso_app.crm.client_secret
  sensitive = true
}

# The company's wiki signs everyone in over SAML: its Entity ID, and its ACS
# URL. The wiki reads Yet's IdP from https://auth.yet.com.br/saml/v2/metadata.
resource "yet_settings_sso_app" "wiki" {
  name          = "Wiki"
  kind          = "saml"
  entity_id     = "https://wiki.acme.com.br/saml"
  redirect_uris = ["https://wiki.acme.com.br/saml/acs"]
  everyone      = true
}
