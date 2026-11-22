package provider

import (
	"context"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

/*
Vault: the company's secrets, kept in its own AWS account — never in Yet.
A vault, its secrets, and the applications it lets read them; what an
application reads, its yet_cloud_application's secret_env names.
*/

// ── yet_vault_vault ──────────────────────────────────────────────────────────

type vaultResource struct{ base }

func newVaultResource() resource.Resource { return &vaultResource{} }

type vaultModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	Kind         types.String `tfsdk:"kind"`
	AWSAccountID types.String `tfsdk:"aws_account_id"`
	Region       types.String `tfsdk:"region"`
	Bucket       types.String `tfsdk:"bucket"`
}

func (r *vaultResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vault_vault"
}

func (r *vaultResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A vault: a set of the company's secrets, a folder of Vault's bucket in its own AWS account — the bucket the " +
			"stack of the account connected for Vault makes, private, encrypted with the account's key, each value's versions kept — never in " +
			"Yet, which knows their names alone. Destroyed, an empty vault is forgotten; the bucket keeps what was in it.\n\n" +
			"-> **Note:** An AWS account connected for Vault first (`yet_settings_aws_account` with `vault` in its `uses`), its stack from template 9 on.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr,
			"name": schema.StringAttribute{
				MarkdownDescription: "Its name, the company's only one so named.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 60)},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "What it is for.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
			},
			"kind": schema.StringAttribute{
				MarkdownDescription: "Who reads it: `systems` (the default), the applications it lets (`yet_vault_grant`); or `shared`, " +
					"people of the company, given it in Yet's apps — a person signed in, never a key. A person's own vault is theirs to make there.",
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString("systems"),
				Validators:    []validator.String{stringvalidator.OneOf("systems", "shared")},
				PlanModifiers: replace,
			},
			"aws_account_id": schema.StringAttribute{
				MarkdownDescription: "The AWS account its secrets are in: the one connected for Vault.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Vault's bucket's region.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"bucket": schema.StringAttribute{
				MarkdownDescription: "Vault's bucket, in the account: its secrets are under `vaults/<kind>/<id>/`, by id, never by name.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (m *vaultModel) from(v *yet.Vault) {
	m.ID, m.Name, m.Description, m.Kind = types.StringValue(v.ID), types.StringValue(v.Name), types.StringValue(v.Description), types.StringValue(v.Kind)
	m.AWSAccountID, m.Region, m.Bucket = types.StringValue(v.AWSAccountID), types.StringValue(v.Region), types.StringValue(v.Bucket)
}

func (r *vaultResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vaultModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.client.Vault.Create(ctx, yet.VaultInput{Name: plan.Name.ValueString(), Kind: plan.Kind.ValueString(), Description: plan.Description.ValueString()})
	if err != nil {
		failed(&resp.Diagnostics, "create the vault", err)
		return
	}
	plan.from(v)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *vaultResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vaultModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.client.Vault.Get(ctx, state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the vault", err)
		}
		return
	}
	state.from(v)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *vaultResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan vaultModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.client.Vault.Update(ctx, plan.ID.ValueString(), strPtr(plan.Name), strPtr(plan.Description))
	if err != nil {
		failed(&resp.Diagnostics, "change the vault", err)
		return
	}
	plan.from(v)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *vaultResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vaultModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if err := r.client.Vault.Delete(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "forget the vault", err)
	}
}

func (r *vaultResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importID(ctx, req, resp)
}

// ── yet_vault_secret ─────────────────────────────────────────────────────────

type vaultSecretResource struct{ base }

func newVaultSecretResource() resource.Resource { return &vaultSecretResource{} }

type vaultSecretModel struct {
	ID      types.String `tfsdk:"id"`
	VaultID types.String `tfsdk:"vault_id"`
	Name    types.String `tfsdk:"name"`
	Note    types.String `tfsdk:"note"`
	Value   types.String `tfsdk:"value"`
	Version types.String `tfsdk:"version"`
}

func (r *vaultSecretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vault_secret"
}

func (r *vaultSecretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A secret of a vault's: its value goes to the vault, in the company's account, and is never read back — Yet " +
			"answers its version alone. Changed, every application reading it is handed it anew. Destroyed, its bucket keeps its versions.\n\n" +
			"-> **Note:** Its value is in Terraform's state, as Terraform keeps what it is given: keep the state where secrets may be.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr,
			"vault_id": schema.StringAttribute{
				MarkdownDescription: "Its vault (`yet_vault_vault`'s `id`).",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Its name, a variable's: letters, digits and `_`, not from a digit.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`), "a variable's name")},
			},
			"note": schema.StringAttribute{
				MarkdownDescription: "What it is.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "Its value, 32 KB at most.",
				Required:            true,
				Sensitive:           true,
			},
			"version": schema.StringAttribute{
				MarkdownDescription: "Its value's version, in the vault.",
				Computed:            true,
			},
		},
	}
}

func (m *vaultSecretModel) from(s *yet.Secret) {
	m.ID, m.VaultID, m.Name = types.StringValue(s.ID), types.StringValue(s.VaultID), types.StringValue(s.Name)
	m.Note, m.Version = types.StringValue(s.Note), types.StringValue(s.Version)
}

func (r *vaultSecretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vaultSecretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.Vault.CreateSecret(ctx, plan.VaultID.ValueString(), yet.SecretInput{Name: strPtr(plan.Name), Note: strPtr(plan.Note), Value: strPtr(plan.Value)})
	if err != nil {
		failed(&resp.Diagnostics, "put the secret", err)
		return
	}
	plan.from(s)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *vaultSecretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vaultSecretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.Vault.GetSecret(ctx, state.VaultID.ValueString(), state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the secret", err)
		}
		return
	}
	// Its value is never read back: what is in the state stays, unless another version was put elsewhere.
	if s.Version != state.Version.ValueString() {
		state.Value = types.StringValue("")
	}
	state.from(s)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *vaultSecretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state vaultSecretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := yet.SecretInput{Name: strPtr(plan.Name), Note: strPtr(plan.Note)}
	if plan.Value.ValueString() != state.Value.ValueString() {
		in.Value = strPtr(plan.Value)
	}
	s, err := r.client.Vault.UpdateSecret(ctx, plan.VaultID.ValueString(), plan.ID.ValueString(), in)
	if err != nil {
		failed(&resp.Diagnostics, "change the secret", err)
		return
	}
	plan.from(s)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *vaultSecretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vaultSecretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if err := r.client.Vault.DeleteSecret(ctx, state.VaultID.ValueString(), state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the secret", err)
	}
}

// ImportState takes "<vault_id>/<secret_id>": its value is set anew at the next apply.
func (r *vaultSecretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	vault, id, ok := strings.Cut(req.ID, "/")
	if !ok || vault == "" || id == "" {
		resp.Diagnostics.AddError("Unexpected import id", "Import a secret as <vault_id>/<secret_id>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vault_id"), vault)...)
}

// ── yet_vault_grant ──────────────────────────────────────────────────────────

type vaultGrantResource struct{ base }

func newVaultGrantResource() resource.Resource { return &vaultGrantResource{} }

type vaultGrantModel struct {
	ID            types.String `tfsdk:"id"`
	VaultID       types.String `tfsdk:"vault_id"`
	ApplicationID types.String `tfsdk:"application_id"`
}

func (r *vaultGrantResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vault_grant"
}

func (r *vaultGrantResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lets an application read a vault's secrets, in the variables its `yet_cloud_application`'s `secret_env` names — " +
			"on a runtime in a Kubernetes Secret beside it, on Lambda in its function's variables, and in its account's Parameter Store, " +
			"under `/yet/<company>/<application id>/`, for it to read itself. Destroyed once it reads none of them.",
		Attributes: map[string]schema.Attribute{
			"id":             idAttr,
			"vault_id":       schema.StringAttribute{MarkdownDescription: "The vault.", Required: true, PlanModifiers: replace},
			"application_id": schema.StringAttribute{MarkdownDescription: "The application.", Required: true, PlanModifiers: replace},
		},
	}
}

func (r *vaultGrantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vaultGrantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Vault.Grant(ctx, plan.VaultID.ValueString(), plan.ApplicationID.ValueString()); err != nil {
		failed(&resp.Diagnostics, "let the application read the vault", err)
		return
	}
	plan.ID = types.StringValue(plan.VaultID.ValueString() + "/" + plan.ApplicationID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *vaultGrantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vaultGrantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.client.Vault.Get(ctx, state.VaultID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the vault", err)
		}
		return
	}
	for _, app := range v.Applications {
		if app == state.ApplicationID.ValueString() {
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *vaultGrantResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
}

func (r *vaultGrantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vaultGrantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if err := r.client.Vault.Revoke(ctx, state.VaultID.ValueString(), state.ApplicationID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "take the application's reading away", err)
	}
}

// ImportState takes "<vault_id>/<application_id>".
func (r *vaultGrantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	vault, app, ok := strings.Cut(req.ID, "/")
	if !ok || vault == "" || app == "" {
		resp.Diagnostics.AddError("Unexpected import id", "Import a grant as <vault_id>/<application_id>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vault_id"), vault)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("application_id"), app)...)
}
