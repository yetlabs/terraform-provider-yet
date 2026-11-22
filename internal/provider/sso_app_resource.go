package provider

import (
	"context"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

/*
A tool of the company's own — its CRM, its intranet — that signs its people
in with Yet, an OpenID Connect client or a SAML service provider. Its kind is
what its client is, and a SAML tool's Entity ID is what Yet knows it by, so a
new kind or Entity ID is a new client (and new credentials); the rest changes
in place.
Its secret is shown only as it is made: the state keeps it, and an imported
tool has none — rotate it in Settings, or make the tool again, to have one.
*/

// ── yet_settings_sso_app ─────────────────────────────────────────────────────

type ssoAppResource struct{ base }

func newSSOAppResource() resource.Resource { return &ssoAppResource{} }

var _ resource.ResourceWithValidateConfig = (*ssoAppResource)(nil)

// entityID is a SAML tool's Entity ID: a URL or a URN, with no spaces.
var entityID = regexp.MustCompile(`^\S+$`)

type ssoAppModel struct {
	ID           types.String `tfsdk:"id"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	EntityID     types.String `tfsdk:"entity_id"`
	Name         types.String `tfsdk:"name"`
	Kind         types.String `tfsdk:"kind"`
	RedirectURIs types.List   `tfsdk:"redirect_uris"`
	Everyone     types.Bool   `tfsdk:"everyone"`
	Allowed      types.Set    `tfsdk:"allowed"`
	InMenu       types.Bool   `tfsdk:"in_menu"`
	HomeURL      types.String `tfsdk:"home_url"`
	PrivacyURL   types.String `tfsdk:"privacy_url"`
	TermsURL     types.String `tfsdk:"terms_url"`
	Icon         types.String `tfsdk:"icon"`
}

func (r *ssoAppResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_sso_app"
}

func (r *ssoAppResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	computed := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A tool of the company's own — its CRM, its intranet — that signs its people in with Yet: " +
			"an OpenID Connect client, its issuer `https://auth.<base domain>`, or a SAML service provider, Yet's IdP " +
			"metadata at `https://auth.<base domain>/saml/v2/metadata`. What it is told says the person's `groups` " +
			"(the addresses of the company's lists they are on), their `role`, `admin` or `member`, and the company's " +
			"`workspace`.\n\n" +
			"-> **Note:** Its `client_secret` is shown only as it is made, and kept in the state; an imported tool has " +
			"none there.\n\n" +
			"!> **Warning:** Destroying it, or changing its kind or Entity ID, stops its \"Sign in with Yet\" — " +
			"a new kind or Entity ID is a new client.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The tool's id.",
				Computed:            true,
				PlanModifiers:       computed,
			},
			"client_id": schema.StringAttribute{
				MarkdownDescription: "Its OpenID Connect client id; a SAML tool's is its `entity_id`.",
				Computed:            true,
				PlanModifiers:       computed,
			},
			"client_secret": schema.StringAttribute{
				MarkdownDescription: "Its client secret, for a `server` tool; null for a `browser` or `saml` one, and for one imported.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       computed,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "What people see as they sign in: `CRM`.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 60)},
			},
			"kind": schema.StringAttribute{
				MarkdownDescription: "`server`, a tool that keeps a secret, `browser`, one in a browser or a phone, " +
					"with none (PKCE), or `saml`, a SAML service provider, with an `entity_id`. Changing it makes the tool again.",
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString("server"),
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.OneOf("server", "browser", "saml")},
			},
			"entity_id": schema.StringAttribute{
				MarkdownDescription: "A `saml` tool's Entity ID, which it needs — `https://wiki.acme.example.com/saml`, up to 1000 " +
					"characters, no spaces. Changing it makes the tool again.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 1000),
					stringvalidator.RegexMatches(entityID, "an Entity ID with no spaces"),
				},
			},
			"redirect_uris": schema.ListAttribute{
				MarkdownDescription: "Where it sends people back after they sign in: https, or http on localhost to try it.",
				ElementType:         types.StringType,
				Required:            true,
				Validators:          []validator.List{listvalidator.SizeBetween(1, 20), listvalidator.UniqueValues()},
			},
			"everyone": schema.BoolAttribute{
				MarkdownDescription: "Whether all of the company may sign in — today and whoever joins — or only those `allowed`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"allowed": schema.SetAttribute{
				MarkdownDescription: "The people and groups (the company's lists) who may sign in, by address, lowercase — " +
					"only the company's.",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Default:     setdefault.StaticValue(types.SetValueMust(types.StringType, []attr.Value{})),
				Validators: []validator.Set{setvalidator.ValueStringsAre(
					stringvalidator.RegexMatches(mailboxAddress, "a lowercase email address"),
				)},
			},
			"in_menu": schema.BoolAttribute{
				MarkdownDescription: "Whether it has a tile in the apps menu of those who may sign in, from the moment they may. On unless turned off.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"home_url": schema.StringAttribute{
				MarkdownDescription: "Where its tile goes: the tool's own address, https. When not given, the site it sends people back to.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 2000)},
			},
			"privacy_url": schema.StringAttribute{
				MarkdownDescription: "Its own privacy policy, https, linked where people sign in to it.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 2000)},
			},
			"terms_url": schema.StringAttribute{
				MarkdownDescription: "Its own terms, https, linked where people sign in to it.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 2000)},
			},
			"icon": schema.StringAttribute{
				MarkdownDescription: "Its tile's image, a data: URL of a PNG, JPEG, WebP or SVG under 75 KB: " +
					"`\"data:image/png;base64,${filebase64(\"crm.png\")}\"`. Its initial when not given.",
				Optional:   true,
				Validators: []validator.String{stringvalidator.LengthBetween(1, 100_000)},
			},
		},
	}
}

// ValidateConfig refuses, at plan, a SAML tool with no Entity ID, and one
// for a tool of OpenID Connect, which has none.
func (r *ssoAppResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m ssoAppModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Kind.IsUnknown() || m.EntityID.IsUnknown() {
		return
	}
	saml := m.Kind.ValueString() == "saml"
	switch {
	case saml && m.EntityID.IsNull():
		resp.Diagnostics.AddAttributeError(path.Root("entity_id"), "A SAML tool needs an entity_id",
			"Give the Entity ID the tool says in its SAML metadata.")
	case !saml && !m.EntityID.IsNull():
		resp.Diagnostics.AddAttributeError(path.Root("entity_id"), "Only a SAML tool has an entity_id",
			"Set kind = \"saml\", or leave entity_id out.")
	}
}

func (r *ssoAppResource) fill(ctx context.Context, m *ssoAppModel, a *yet.SSOApp, d *diagsSink) {
	m.ID = types.StringValue(a.ID)
	m.ClientID = types.StringValue(a.ClientID)
	m.EntityID = orNull(a.EntityID)
	m.Name = types.StringValue(a.Name)
	m.Kind = types.StringValue(a.Kind)
	m.RedirectURIs = listOf(ctx, a.RedirectURIs, &d.d)
	m.Everyone = types.BoolValue(a.Everyone)
	m.Allowed = setOf(ctx, a.Allowed, &d.d)
	m.InMenu = types.BoolValue(a.InMenu)
	// yet writes the address as a browser does — https://crm.acme.example.com/ —
	// so the one given stays when that is all that differs.
	if !sameURL(m.HomeURL.ValueString(), a.HomeURL) {
		m.HomeURL = orNull(a.HomeURL)
	}
	m.Icon = orNull(a.Icon)
	if !sameURL(m.PrivacyURL.ValueString(), a.PrivacyURL) {
		m.PrivacyURL = orNull(a.PrivacyURL)
	}
	if !sameURL(m.TermsURL.ValueString(), a.TermsURL) {
		m.TermsURL = orNull(a.TermsURL)
	}
}

// sameURL is whether two addresses differ only as a browser would write them.
func sameURL(given, kept string) bool {
	if given == "" || kept == "" {
		return given == kept
	}
	return strings.EqualFold(strings.TrimSuffix(given, "/"), strings.TrimSuffix(kept, "/"))
}

func (r *ssoAppResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ssoAppModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	uris := stringsOf(ctx, plan.RedirectURIs, &resp.Diagnostics)
	allowed := stringsOf(ctx, plan.Allowed, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	a, secret, err := r.client.Settings.SSOApps.Create(ctx, yet.NewSSOApp{
		Name:         plan.Name.ValueString(),
		Kind:         plan.Kind.ValueString(),
		EntityID:     plan.EntityID.ValueString(),
		RedirectURIs: *uris,
		Everyone:     plan.Everyone.ValueBool(),
		Allowed:      *allowed,
		InMenu:       plan.InMenu.ValueBoolPointer(),
		HomeURL:      plan.HomeURL.ValueString(),
		Icon:         plan.Icon.ValueString(),
		PrivacyURL:   plan.PrivacyURL.ValueString(),
		TermsURL:     plan.TermsURL.ValueString(),
	})
	if err != nil {
		failed(&resp.Diagnostics, "make the tool", err)
		return
	}
	d := &diagsSink{}
	r.fill(ctx, &plan, a, d)
	plan.ClientSecret = orNull(secret)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ssoAppResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ssoAppModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	a, _, err := r.client.Settings.SSOApps.Get(ctx, state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the tool", err)
		}
		return
	}
	d := &diagsSink{}
	r.fill(ctx, &state, a, d)
	if state.ClientSecret.IsUnknown() {
		state.ClientSecret = types.StringNull()
	}
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ssoAppResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ssoAppModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	uris := stringsOf(ctx, plan.RedirectURIs, &resp.Diagnostics)
	allowed := stringsOf(ctx, plan.Allowed, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.Settings.SSOApps.Update(ctx, state.ID.ValueString(), yet.SSOAppChange{
		Name:         plan.Name.ValueStringPointer(),
		RedirectURIs: *uris,
		Everyone:     plan.Everyone.ValueBoolPointer(),
		Allowed:      *allowed,
		InMenu:       plan.InMenu.ValueBoolPointer(),
		HomeURL:      yet.Ptr(plan.HomeURL.ValueString()),
		Icon:         yet.Ptr(plan.Icon.ValueString()),
		PrivacyURL:   yet.Ptr(plan.PrivacyURL.ValueString()),
		TermsURL:     yet.Ptr(plan.TermsURL.ValueString()),
	})
	if err != nil {
		failed(&resp.Diagnostics, "change the tool", err)
		return
	}
	d := &diagsSink{}
	r.fill(ctx, &plan, a, d)
	plan.ClientSecret = state.ClientSecret
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ssoAppResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ssoAppModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Settings.SSOApps.Delete(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the tool", err)
	}
}

func (r *ssoAppResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
