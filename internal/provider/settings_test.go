package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// settingsResources are the company's mail addresses, sending, AWS, storage,
// logo, domains and tools that sign in with Yet — what the provider registers among its Settings.
var settingsResources = []func() resource.Resource{
	newMailAliasResource,
	newMailListResource,
	newMailCatchAllResource,
	newSendingResource,
	newAWSAccountResource,
	newAWSAccountRoleResource,
	newStorageResource,
	newLogoResource,
	newDomainClaimResource,
	newDomainResource,
	newSubdomainResource,
	newSharedMailboxResource,
	newSSOAppResource,
}

func TestTheSettingsSchemasHold(t *testing.T) {
	server, err := factories["yet"]()
	if err != nil {
		t.Fatal(err)
	}
	got, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range got.Diagnostics {
		t.Errorf("%s: %s", d.Summary, d.Detail)
	}
	for _, f := range settingsResources {
		var m resource.MetadataResponse
		f().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "yet"}, &m)
		if got.ResourceSchemas[m.TypeName] == nil {
			t.Errorf("%s has no schema", m.TypeName)
		}
	}
}

/*
fakeSettings is a company's Settings as these resources reach them, kept in
memory: one person, Ana, at acme.example.org, and whatever the tests make.
*/
type fakeSettings struct {
	mu       sync.Mutex
	aliases  map[string]bool // "<account>/<name>"
	lists    map[string]map[string]any
	catchAll map[string]string
	sending  string
	aws      map[string]any
	storage  map[string]any
	logo     map[string]any
	domains  map[string]bool // name → mail
	claims   map[string]string
	subs     map[string]string         // name → parent
	shared   map[string]map[string]any // address → the shared mailbox
	sso      map[string]map[string]any // id → the tool that signs in with Yet
	emails   []map[string]any          // as sent, with the Idempotency-Key as "key"
	next     int
}

func newFakeSettings(t *testing.T) (*httptest.Server, *fakeSettings) {
	f := &fakeSettings{
		aliases:  map[string]bool{},
		lists:    map[string]map[string]any{},
		catchAll: map[string]string{},
		sending:  "yet",
		domains:  map[string]bool{"acme.example.org": true},
		claims:   map[string]string{},
		subs:     map[string]string{},
		shared:   map[string]map[string]any{},
		sso:      map[string]map[string]any{},
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return srv, f
}

func (f *fakeSettings) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+testKey {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	str := func(k string) string { s, _ := in[k].(string); return s }
	strs := func(k string) []string {
		out := []string{}
		for _, x := range in[k].([]any) {
			out = append(out, x.(string))
		}
		return out
	}
	p := strings.TrimPrefix(r.URL.Path, "/v1")
	route := r.Method + " " + p
	switch {
	case route == "GET /settings/people":
		reply(200, map[string]any{
			"people":  []map[string]any{{"id": "p1", "name": "ana", "email": "ana@acme.example.org", "full_name": "Ana Costa"}},
			"members": []any{},
		})
	case route == "GET /mail/domains/acme.example.org/addresses":
		aliases := []map[string]any{}
		for k := range f.aliases {
			account, name, _ := strings.Cut(k, "/")
			aliases = append(aliases, map[string]any{"name": name, "address": name + "@acme.example.org", "account_id": account, "owner": "ana@acme.example.org"})
		}
		lists := []map[string]any{}
		for _, l := range f.lists {
			lists = append(lists, l)
		}
		var catchAll any
		if a, ok := f.catchAll["acme.example.org"]; ok {
			catchAll = a
		}
		reply(200, map[string]any{"aliases": aliases, "lists": lists, "catch_all": catchAll})
	case r.Method == "POST" && strings.HasPrefix(p, "/mail/accounts/") && strings.HasSuffix(p, "/aliases"):
		account := strings.TrimSuffix(strings.TrimPrefix(p, "/mail/accounts/"), "/aliases")
		f.aliases[account+"/"+str("name")] = true
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "DELETE" && strings.HasPrefix(p, "/mail/accounts/") && strings.Contains(p, "/aliases/"):
		account, name, _ := strings.Cut(strings.TrimPrefix(p, "/mail/accounts/"), "/aliases/")
		delete(f.aliases, account+"/"+name)
		w.WriteHeader(http.StatusNoContent)
	case route == "PUT /mail/domains/acme.example.org/catch-all":
		if a := str("address"); a != "" {
			f.catchAll["acme.example.org"] = a
		} else {
			delete(f.catchAll, "acme.example.org")
		}
		reply(200, map[string]any{"ok": true})
	case route == "POST /mail/domains/acme.example.org/lists":
		f.next++
		id := fmt.Sprintf("l%d", f.next)
		f.lists[id] = map[string]any{"id": id, "name": str("name"), "address": str("name") + "@acme.example.org", "description": str("description"), "recipients": strs("recipients")}
		reply(200, f.lists[id])
	case strings.HasPrefix(p, "/mail/lists/") && f.lists[strings.TrimPrefix(p, "/mail/lists/")] != nil:
		id := strings.TrimPrefix(p, "/mail/lists/")
		switch r.Method {
		case "GET":
			reply(200, f.lists[id])
		case "PATCH":
			f.lists[id]["recipients"] = strs("recipients")
			if d, ok := in["description"].(string); ok {
				f.lists[id]["description"] = d
			}
			reply(200, map[string]any{"ok": true})
		case "DELETE":
			delete(f.lists, id)
			reply(200, map[string]any{"ok": true})
		}
	case route == "GET /mail/sending":
		reply(200, map[string]any{"sending": f.sending, "ses_allowed": true})
	case route == "PUT /mail/sending":
		f.sending = str("sending")
		reply(200, map[string]any{"warnings": []string{}})
	case route == "GET /connections/aws":
		accounts := []map[string]any{}
		if f.aws != nil {
			accounts = append(accounts, f.aws)
		}
		reply(200, map[string]any{"accounts": accounts, "instance_types": []string{"t3.small"}})
	case route == "GET /connections/aws/launch":
		reply(200, map[string]any{
			"template":  "https://yet-public-210987654321.s3.amazonaws.com/aws/connection-v1.yaml",
			"principal": "arn:aws:iam::210987654321:role/yet-server",
		})
	case route == "POST /connections/aws":
		f.aws = map[string]any{
			"id": "aws_1", "name": str("name"), "region": str("region"), "uses": in["uses"], "role_arn": "",
			"external_id": "Zq3vJ8mL1xT0bN6cR4wY9hK2pD7sF5aE",
		}
		reply(200, f.aws)
	case route == "PATCH /connections/aws/aws_1":
		if v, ok := in["role_arn"]; ok {
			f.aws["role_arn"] = v
		}
		if v, ok := in["name"]; ok {
			f.aws["name"] = v
		}
		if v, ok := in["uses"]; ok {
			f.aws["uses"] = v
		}
		reply(200, map[string]any{"warnings": []string{}, "id": "aws_1", "name": f.aws["name"], "region": f.aws["region"],
			"uses": f.aws["uses"], "role_arn": f.aws["role_arn"], "external_id": f.aws["external_id"]})
	case route == "DELETE /connections/aws/aws_1":
		f.aws = nil
		reply(200, map[string]any{"ok": true})
	case route == "GET /drive/storage":
		reply(200, map[string]any{"storage": f.storage})
	case route == "PUT /drive/storage":
		if f.aws == nil {
			reply(409, map[string]string{"error": "Connect the AWS account first."})
			return
		}
		f.storage = map[string]any{"bucket": str("bucket"), "prefix": str("prefix")}
		reply(200, map[string]any{"ok": true})
	case route == "DELETE /drive/storage":
		f.storage = nil
		reply(200, map[string]any{"ok": true})
	case route == "GET /settings/logo":
		reply(200, map[string]any{"logo": f.logo})
	case route == "PUT /settings/logo":
		f.logo = map[string]any{"content_type": str("content_type"), "data": str("data")}
		reply(200, map[string]any{"ok": true})
	case route == "DELETE /settings/logo":
		f.logo = nil
		reply(200, map[string]any{"ok": true})
	case route == "GET /settings/domains":
		domains := []map[string]any{}
		for name, mail := range f.domains {
			domains = append(domains, map[string]any{"name": name, "mail": mail})
		}
		// A subdomain is one of the company's domains, its parent said.
		for name, parent := range f.subs {
			domains = append(domains, map[string]any{"name": name, "mail": true, "parent": parent})
		}
		claims := []map[string]any{}
		for name, txt := range f.claims {
			claims = append(claims, map[string]any{"domain": name, "txt": txt, "created_at": "2026-10-24 12:00:00"})
		}
		reply(200, map[string]any{"domains": domains, "claims": claims})
	case route == "POST /settings/domains" && in["subdomain"] == true:
		name := str("name")
		if !strings.HasSuffix(name, ".acme.example.org") {
			reply(400, map[string]string{"error": name + " is not under any of Acme’s domains: add it under Domains first."})
			return
		}
		if _, ok := f.subs[name]; ok {
			reply(409, map[string]string{"error": name + " is the company’s already."})
			return
		}
		f.subs[name] = "acme.example.org"
		reply(200, map[string]any{"name": name, "parent": "acme.example.org", "mail": true, "warnings": []string{}})
	case route == "POST /settings/domains":
		name := str("name")
		if _, ok := f.domains[name]; ok {
			reply(409, map[string]string{"error": name + " is the company’s already."})
			return
		}
		f.claims[name] = "yet-verification=" + name
		reply(200, map[string]any{"domain": name, "txt": f.claims[name]})
	case r.Method == "DELETE" && strings.HasPrefix(p, "/settings/domains/") && f.subs[strings.TrimPrefix(p, "/settings/domains/")] != "":
		delete(f.subs, strings.TrimPrefix(p, "/settings/domains/"))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "DELETE" && strings.HasPrefix(p, "/settings/domains/"):
		delete(f.claims, strings.TrimPrefix(p, "/settings/domains/"))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "POST" && strings.HasPrefix(p, "/settings/domains/") && strings.HasSuffix(p, "/verify"):
		name := strings.TrimSuffix(strings.TrimPrefix(p, "/settings/domains/"), "/verify")
		if _, ok := f.claims[name]; !ok {
			reply(404, map[string]string{"error": name + " was not asked for."})
			return
		}
		delete(f.claims, name)
		f.domains[name] = false
		reply(200, map[string]any{"domain": name, "how": "txt"})
	case r.Method == "PUT" && strings.HasPrefix(p, "/mail/domains/") && strings.Count(p, "/") == 3:
		f.domains[strings.TrimPrefix(p, "/mail/domains/")] = true
		reply(200, map[string]any{"warnings": []string{}})
	case r.Method == "DELETE" && strings.HasPrefix(p, "/mail/domains/"):
		delete(f.domains, strings.TrimPrefix(p, "/mail/domains/"))
		w.WriteHeader(http.StatusNoContent)
	case route == "GET /mail/shared-mailboxes":
		boxes := []map[string]any{}
		for _, m := range f.shared {
			boxes = append(boxes, m)
		}
		reply(200, boxes)
	case route == "POST /mail/shared-mailboxes":
		address := str("address")
		local, domain, _ := strings.Cut(address, "@")
		if domain != "acme.example.org" && f.subs[domain] == "" {
			reply(404, map[string]string{"error": domain + " has no mail yet."})
			return
		}
		name := str("name")
		if name == "" {
			name = local
		}
		f.next++
		f.shared[address] = map[string]any{"id": fmt.Sprintf("g%d", f.next), "address": address, "name": name, "members": strs("members")}
		reply(200, f.shared[address])
	case strings.HasPrefix(p, "/mail/shared-mailboxes/"):
		address := strings.TrimPrefix(p, "/mail/shared-mailboxes/")
		m := f.shared[address]
		if m == nil {
			reply(404, map[string]string{"error": address + " is not one of Acme’s shared mailboxes."})
			return
		}
		switch r.Method {
		case "PATCH":
			if _, ok := in["name"]; ok {
				m["name"] = str("name")
			}
			if _, ok := in["members"]; ok {
				m["members"] = strs("members")
			}
			reply(200, m)
		case "DELETE":
			delete(f.shared, address)
			reply(200, map[string]any{"ok": true})
		}
	case route == "GET /iam/sso/tools":
		apps := []map[string]any{}
		for _, a := range f.sso {
			apps = append(apps, a)
		}
		reply(200, apps)
	case route == "POST /iam/sso/tools":
		f.next++
		id := fmt.Sprintf("sa_%d", f.next)
		kind := str("kind")
		if kind == "" {
			kind = "server"
		}
		a := map[string]any{
			"id": id, "client_id": fmt.Sprintf("%d@acme", 1000+f.next), "kind": kind,
			"everyone": false, "allowed": []string{}, "in_menu": true, "home_url": nil, "icon": nil,
			"entity_id": nil, "created_by": "ana@acme.example.org", "created_at": "2026-10-20 14:00:00",
		}
		if kind == "saml" {
			a["entity_id"], a["client_id"] = str("entity_id"), str("entity_id")
		}
		f.sso[id] = a
		f.ssoChange(a, in)
		var secret any
		if kind == "server" {
			secret = "s3cr3t-" + id
		}
		reply(201, map[string]any{"app": a, "client_secret": secret})
	case strings.HasPrefix(p, "/iam/sso/tools/"):
		id, secret := strings.CutSuffix(strings.TrimPrefix(p, "/iam/sso/tools/"), "/secret")
		id, signIns := strings.CutSuffix(id, "/sign-ins")
		a := f.sso[id]
		if a == nil {
			reply(404, map[string]string{"error": "No such tool."})
			return
		}
		switch {
		case secret && a["kind"] != "server":
			reply(400, map[string]string{"error": "An app in a browser or a phone has no secret."})
		case secret:
			reply(200, map[string]any{"client_secret": "s3cr3t-again"})
		case signIns:
			reply(200, []any{})
		case r.Method == "GET":
			reply(200, a)
		case r.Method == "PATCH":
			f.ssoChange(a, in)
			reply(200, a)
		case r.Method == "DELETE":
			delete(f.sso, id)
			reply(200, map[string]any{"ok": true})
		}
	case route == "POST /mail/emails":
		in["key"] = r.Header.Get("Idempotency-Key")
		f.emails = append(f.emails, in)
		reply(200, map[string]any{"id": fmt.Sprintf("em_%d", len(f.emails))})
	default:
		reply(404, map[string]string{"error": "No such thing."})
	}
}

// ssoChange is a tool changed as yet changes it: in what is sent, its home
// written as a browser writes it.
func (f *fakeSettings) ssoChange(a, in map[string]any) {
	for _, k := range []string{"name", "kind", "redirect_uris", "everyone", "allowed", "in_menu", "home_url", "icon"} {
		if v, ok := in[k]; ok {
			a[k] = v
		}
	}
	if home, ok := a["home_url"].(string); ok && strings.Count(home, "/") == 2 {
		a["home_url"] = home + "/"
	}
}

// A subdomain made with its parent, imported by its name, and gone on destroy.
func TestMailSubdomain(t *testing.T) {
	srv, f := newFakeSettings(t)
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			if len(f.subs) != 0 {
				return fmt.Errorf("subdomains left: %v", f.subs)
			}
			return nil
		},
		Steps: []tfresource.TestStep{
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_mail_subdomain" "notices" {
  name = "avisos.acme.example.org"
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_settings_mail_subdomain.notices", tfjsonpath.New("id"), knownvalue.StringExact("avisos.acme.example.org")),
					statecheck.ExpectKnownValue("yet_settings_mail_subdomain.notices", tfjsonpath.New("parent"), knownvalue.StringExact("acme.example.org")),
				},
			},
			{
				ResourceName:      "yet_settings_mail_subdomain.notices",
				ImportState:       true,
				ImportStateId:     "avisos.acme.example.org",
				ImportStateVerify: true,
			},
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_mail_subdomain" "notices" {
  name = "globex.example"
}`,
				ExpectError: regexp.MustCompile(`is not under any`),
			},
		},
	})
}

// A shared mailbox named for its address when not named, renamed and given to
// others in place, imported by its address, and gone on destroy.
func TestMailSharedMailbox(t *testing.T) {
	srv, f := newFakeSettings(t)
	box := "yet_settings_mail_shared_mailbox.orders"
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			if len(f.shared) != 0 {
				return fmt.Errorf("shared mailboxes left: %v", f.shared)
			}
			return nil
		},
		Steps: []tfresource.TestStep{
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_mail_shared_mailbox" "orders" {
  address = "pedidos@acme.example.org"
  members = ["ana@acme.example.org"]
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(box, tfjsonpath.New("id"), knownvalue.StringExact("pedidos@acme.example.org")),
					statecheck.ExpectKnownValue(box, tfjsonpath.New("name"), knownvalue.StringExact("pedidos")),
				},
			},
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_mail_shared_mailbox" "orders" {
  address = "pedidos@acme.example.org"
  name    = "Pedidos"
  members = ["ana@acme.example.org", "ops@acme.example.org"]
}`,
				ConfigPlanChecks: tfresource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(box, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(box, tfjsonpath.New("name"), knownvalue.StringExact("Pedidos")),
					statecheck.ExpectKnownValue(box, tfjsonpath.New("members"), knownvalue.SetSizeExact(2)),
				},
			},
			{
				ResourceName:      box,
				ImportState:       true,
				ImportStateId:     "pedidos@acme.example.org",
				ImportStateVerify: true,
			},
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_mail_shared_mailbox" "orders" {
  address = "pedidos@example.com"
  members = []
}`,
				ExpectError: regexp.MustCompile(`has no mail yet`),
			},
		},
	})
}

// A SAML tool with no Entity ID refused at plan; a tool made with its secret,
// given everyone and a tile in place — its home kept as written — imported by
// its id without its secret, made again for a new kind and again as SAML,
// with no secret, and gone on destroy.
func TestSSOApp(t *testing.T) {
	srv, f := newFakeSettings(t)
	app := "yet_settings_sso_app.crm"
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			if len(f.sso) != 0 {
				return fmt.Errorf("tools left: %v", f.sso)
			}
			return nil
		},
		Steps: []tfresource.TestStep{
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_sso_app" "crm" {
  name          = "Wiki"
  kind          = "saml"
  redirect_uris = ["https://wiki.acme.example.org/saml/acs"]
}`,
				ExpectError: regexp.MustCompile(`A SAML tool needs an entity_id`),
			},
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_sso_app" "crm" {
  name          = "CRM"
  redirect_uris = ["https://crm.acme.example.org/auth/callback"]
  allowed       = ["ana@acme.example.org"]
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(app, tfjsonpath.New("id"), knownvalue.StringExact("sa_1")),
					statecheck.ExpectKnownValue(app, tfjsonpath.New("kind"), knownvalue.StringExact("server")),
					statecheck.ExpectKnownValue(app, tfjsonpath.New("client_id"), knownvalue.StringExact("1001@acme")),
					statecheck.ExpectKnownValue(app, tfjsonpath.New("client_secret"), knownvalue.StringExact("s3cr3t-sa_1")),
				},
			},
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_sso_app" "crm" {
  name          = "CRM"
  redirect_uris = ["https://crm.acme.example.org/auth/callback"]
  everyone      = true
  in_menu       = true
  home_url      = "https://crm.acme.example.org"
}`,
				ConfigPlanChecks: tfresource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(app, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(app, tfjsonpath.New("everyone"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(app, tfjsonpath.New("allowed"), knownvalue.SetSizeExact(0)),
					statecheck.ExpectKnownValue(app, tfjsonpath.New("home_url"), knownvalue.StringExact("https://crm.acme.example.org")),
					statecheck.ExpectKnownValue(app, tfjsonpath.New("client_secret"), knownvalue.StringExact("s3cr3t-sa_1")),
				},
			},
			{
				ResourceName:            app,
				ImportState:             true,
				ImportStateId:           "sa_1",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"client_secret", "home_url"},
			},
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_sso_app" "crm" {
  name          = "CRM"
  kind          = "browser"
  redirect_uris = ["http://localhost:5173/callback"]
}`,
				ConfigPlanChecks: tfresource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(app, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(app, tfjsonpath.New("client_secret"), knownvalue.Null()),
				},
			},
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_sso_app" "crm" {
  name          = "Wiki"
  kind          = "saml"
  entity_id     = "https://wiki.acme.example.org/saml"
  redirect_uris = ["https://wiki.acme.example.org/saml/acs"]
}`,
				ConfigPlanChecks: tfresource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(app, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(app, tfjsonpath.New("entity_id"), knownvalue.StringExact("https://wiki.acme.example.org/saml")),
					statecheck.ExpectKnownValue(app, tfjsonpath.New("client_id"), knownvalue.StringExact("https://wiki.acme.example.org/saml")),
					statecheck.ExpectKnownValue(app, tfjsonpath.New("client_secret"), knownvalue.Null()),
				},
			},
			{
				ResourceName:            app,
				ImportState:             true,
				ImportStateIdFunc:       func(s *terraform.State) (string, error) { return s.RootModule().Resources[app].Primary.ID, nil },
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"client_secret"},
			},
		},
	})
}

// The email goes as the subdomain is made: its tags by name, its attachment's
// bytes, and its Idempotency-Key as a header.
func TestSendEmail(t *testing.T) {
	srv, f := newFakeSettings(t)
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []tfresource.TestStep{
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_mail_subdomain" "notices" {
  name = "avisos.acme.example.org"
}

action "yet_settings_mail_send_email" "ready" {
  config {
    from            = "Acme <ops@${yet_settings_mail_subdomain.notices.name}>"
    to              = ["ana@acme.example.org"]
    subject         = "Notices are ready"
    text            = "avisos.acme.example.org sends now."
    tags            = { source = "terraform", kind = "notice" }
    idempotency_key = "notices-ready"

    attachments = [{
      filename       = "notes.txt"
      content_base64 = base64encode("hello")
    }]
  }
}

resource "terraform_data" "ready" {
  input = yet_settings_mail_subdomain.notices.id

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.yet_settings_mail_send_email.ready]
    }
  }
}`,
				Check: func(*terraform.State) error {
					if len(f.emails) != 1 {
						return fmt.Errorf("sent %d emails", len(f.emails))
					}
					e := f.emails[0]
					tags, _ := json.Marshal(e["tags"])
					at := e["attachments"].([]any)[0].(map[string]any)
					if e["key"] != "notices-ready" || e["from"] != "Acme <ops@avisos.acme.example.org>" ||
						string(tags) != `[{"name":"kind","value":"notice"},{"name":"source","value":"terraform"}]` ||
						at["content"] != "aGVsbG8=" || e["html"] != nil {
						return fmt.Errorf("sent as: %+v", e)
					}
					return nil
				},
			},
		},
	})
}

// A list made, its recipients changed whole, imported by its id, and gone.
func TestMailList(t *testing.T) {
	srv, f := newFakeSettings(t)
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			if len(f.lists) != 0 {
				return fmt.Errorf("lists left: %v", f.lists)
			}
			return nil
		},
		Steps: []tfresource.TestStep{
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_mail_list" "finance" {
  domain     = "acme.example.org"
  name       = "financeiro"
  recipients = ["ana@acme.example.org"]
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_settings_mail_list.finance", tfjsonpath.New("address"), knownvalue.StringExact("financeiro@acme.example.org")),
					statecheck.ExpectKnownValue("yet_settings_mail_list.finance", tfjsonpath.New("description"), knownvalue.StringExact("")),
				},
			},
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_mail_list" "finance" {
  domain      = "acme.example.org"
  name        = "financeiro"
  description = "Invoices and receipts."
  recipients  = ["ana@acme.example.org", "accounting@example.com"]
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_settings_mail_list.finance", tfjsonpath.New("recipients"), knownvalue.SetSizeExact(2)),
					statecheck.ExpectKnownValue("yet_settings_mail_list.finance", tfjsonpath.New("id"), knownvalue.StringExact("l1")),
				},
			},
			{
				ResourceName:      "yet_settings_mail_list.finance",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// An alias at Ana's own domain, and the domain's catch-all: made, imported, gone.
func TestMailAliasAndCatchAll(t *testing.T) {
	srv, f := newFakeSettings(t)
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			if len(f.aliases) != 0 || len(f.catchAll) != 0 {
				return fmt.Errorf("left: %v %v", f.aliases, f.catchAll)
			}
			return nil
		},
		Steps: []tfresource.TestStep{
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_mail_alias" "contact" {
  person_id = "p1"
  name      = "contato"
}

resource "yet_settings_mail_catch_all" "acme" {
  domain  = "acme.example.org"
  address = yet_settings_mail_alias.contact.address
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_settings_mail_alias.contact", tfjsonpath.New("id"), knownvalue.StringExact("p1/contato")),
					statecheck.ExpectKnownValue("yet_settings_mail_alias.contact", tfjsonpath.New("domain"), knownvalue.StringExact("acme.example.org")),
					statecheck.ExpectKnownValue("yet_settings_mail_catch_all.acme", tfjsonpath.New("address"), knownvalue.StringExact("contato@acme.example.org")),
				},
			},
			{
				ResourceName:      "yet_settings_mail_alias.contact",
				ImportState:       true,
				ImportStateId:     "p1/contato",
				ImportStateVerify: true,
			},
			{
				ResourceName:      "yet_settings_mail_catch_all.acme",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// An account, its role, then Drive's bucket on it: all let go on destroy.
func TestAWSAndStorage(t *testing.T) {
	srv, f := newFakeSettings(t)
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			if f.aws != nil || f.storage != nil {
				return fmt.Errorf("left: %v %v", f.aws, f.storage)
			}
			return nil
		},
		Steps: []tfresource.TestStep{
			{
				Config: providerFor(srv.URL) + `
data "yet_settings_aws_launch" "acme" {}

resource "yet_settings_aws_account" "production" {
  name   = "Production"
  region = "us-east-1"
  uses   = ["mail", "drive", "databases"]
}

resource "yet_settings_aws_account_role" "production" {
  account_id = yet_settings_aws_account.production.id
  role_arn   = "arn:aws:iam::123456789012:role/yet-acme-production-Role-1"
}

resource "yet_settings_sending" "acme" {
  sending = "own"
}

resource "yet_settings_storage" "acme" {
  bucket = "acme-drive"
  prefix = "yet/drive"

  depends_on = [yet_settings_aws_account_role.production]
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_settings_aws_account.production", tfjsonpath.New("id"), knownvalue.StringExact("aws_1")),
					statecheck.ExpectKnownValue("data.yet_settings_aws_launch.acme", tfjsonpath.New("principal"),
						knownvalue.StringExact("arn:aws:iam::210987654321:role/yet-server")),
					statecheck.ExpectKnownValue("yet_settings_sending.acme", tfjsonpath.New("ses_allowed"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue("yet_settings_storage.acme", tfjsonpath.New("prefix"), knownvalue.StringExact("yet/drive")),
				},
			},
			{
				ResourceName:            "yet_settings_aws_account.production",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"external_id"},
			},
			{
				ResourceName:      "yet_settings_storage.acme",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// The logo from base64, changed, then from a file, and Yet's mark back on destroy.
func TestLogo(t *testing.T) {
	srv, f := newFakeSettings(t)
	file := filepath.Join(t.TempDir(), "logo.webp")
	if err := os.WriteFile(file, []byte("RIFF"), 0o600); err != nil {
		t.Fatal(err)
	}
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			if f.logo != nil {
				return fmt.Errorf("the logo stayed: %v", f.logo)
			}
			return nil
		},
		Steps: []tfresource.TestStep{
			{
				// <svg/>, told an SVG by its start.
				Config: providerFor(srv.URL) + `resource "yet_settings_logo" "acme" { content_base64 = "PHN2Zy8+" }`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_settings_logo.acme", tfjsonpath.New("content_type"), knownvalue.StringExact("image/svg+xml")),
					statecheck.ExpectKnownValue("yet_settings_logo.acme", tfjsonpath.New("sha256"), knownvalue.StringExact(digest([]byte("<svg/>")))),
				},
			},
			{
				Config: providerFor(srv.URL) + `resource "yet_settings_logo" "acme" { content_base64 = "iVBORw0KGgo=" }`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_settings_logo.acme", tfjsonpath.New("content_type"), knownvalue.StringExact("image/png")),
				},
			},
			{
				// From a file: its type by its extension.
				Config: providerFor(srv.URL) + `resource "yet_settings_logo" "acme" { source = "` + file + `" }`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_settings_logo.acme", tfjsonpath.New("content_type"), knownvalue.StringExact("image/webp")),
					statecheck.ExpectKnownValue("yet_settings_logo.acme", tfjsonpath.New("sha256"), knownvalue.StringExact(digest([]byte("RIFF")))),
				},
			},
		},
	})
}

// A domain asked for, shown, and its mail turned on; the domain with mail goes on destroy.
func TestDomainClaimAndDomain(t *testing.T) {
	every := verifyEvery
	verifyEvery = 0
	t.Cleanup(func() { verifyEvery = every })
	srv, f := newFakeSettings(t)
	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			if _, ok := f.domains["acme-br.example.com"]; ok || len(f.claims) != 0 {
				return fmt.Errorf("left: %v %v", f.domains, f.claims)
			}
			return nil
		},
		Steps: []tfresource.TestStep{
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_domain_claim" "br" {
  name = "acme-br.example.com"
}

resource "yet_settings_domain" "br" {
  name = yet_settings_domain_claim.br.name
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_settings_domain_claim.br", tfjsonpath.New("txt"), knownvalue.StringExact("yet-verification=acme-br.example.com")),
					statecheck.ExpectKnownValue("yet_settings_domain.br", tfjsonpath.New("verified_by"), knownvalue.StringExact("txt")),
					statecheck.ExpectKnownValue("yet_settings_domain.br", tfjsonpath.New("mail"), knownvalue.Bool(false)),
				},
			},
			{
				Config: providerFor(srv.URL) + `
resource "yet_settings_domain_claim" "br" {
  name = "acme-br.example.com"
}

resource "yet_settings_domain" "br" {
  name = yet_settings_domain_claim.br.name
  mail = true
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_settings_domain.br", tfjsonpath.New("mail"), knownvalue.Bool(true)),
				},
			},
			{
				ResourceName:            "yet_settings_domain.br",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"verified_by"},
			},
		},
	})
}
