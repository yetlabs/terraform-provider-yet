# The company's CRM signs its sales team in with Yet, and has a tile in
# their apps menu. Its client goes to the CRM's configuration.
resource "yet_settings_sso_app" "crm" {
  name          = "CRM"
  redirect_uris = ["https://crm.acme-br.example.com/auth/callback"]
  allowed = [
    yet_settings_person.ana.email,
    "vendas@acme-br.example.com",
  ]
  in_menu     = true
  home_url    = "https://crm.acme-br.example.com"
  privacy_url = "https://crm.acme-br.example.com/privacy"
  terms_url   = "https://crm.acme-br.example.com/terms"
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
  entity_id     = "https://wiki.acme-br.example.com/saml"
  redirect_uris = ["https://wiki.acme-br.example.com/saml/acs"]
  everyone      = true
}
