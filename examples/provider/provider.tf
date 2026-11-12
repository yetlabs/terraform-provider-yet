terraform {
  required_providers {
    yet = {
      source = "yetlabs/yet"
    }
  }
}

# The key comes from YET_API_KEY when api_key is left out. Make one in Yet:
# Preferences → API keys.
provider "yet" {}
