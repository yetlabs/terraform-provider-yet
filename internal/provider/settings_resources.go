package provider

import (
	"context"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

/*
A company's Settings. The company itself is never made or removed here —
yet's operator does that — so yet_workspace and yet_workspace_apps take
hold of what is there: created, they set it; destroyed, they let go and
leave it as it is.
*/

// ── yet_settings_workspace ───────────────────────────────────────────────────

type workspaceResource struct{ base }

func newWorkspaceResource() resource.Resource { return &workspaceResource{} }

type workspaceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	OpenJoin    types.Bool   `tfsdk:"open_join"`
	Domains     types.List   `tfsdk:"domains"`
	MailDomains types.List   `tfsdk:"mail_domains"`
}

func (r *workspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_workspace"
}

func (r *workspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The company the API key belongs to, as its Settings have it.\n\n" +
			"~> **Note:** There is one company per key: creating this resource takes hold of it, and destroying it leaves the company as it is.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The company's slug: its address, `<slug>.yet.com.br`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Shown at the top of every app and on the sign-in page.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"open_join": schema.BoolAttribute{
				MarkdownDescription: "Whether anyone at the company's domains may join by themselves.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"domains": schema.ListAttribute{
				MarkdownDescription: "The company's verified domains: who may sign in.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"mail_domains": schema.ListAttribute{
				MarkdownDescription: "Its domains with mail here: where mailboxes can be made.",
				ElementType:         types.StringType,
				Computed:            true,
			},
		},
	}
}

func (r *workspaceResource) apply(ctx context.Context, plan workspaceModel) (*workspaceModel, error) {
	w, err := r.client.Settings.Workspace.Get(ctx)
	if err != nil {
		return nil, err
	}
	if w.Name != plan.Name.ValueString() {
		if err := r.client.Settings.Workspace.Rename(ctx, plan.Name.ValueString()); err != nil {
			return nil, err
		}
	}
	if known(plan.OpenJoin) && w.OpenJoin != plan.OpenJoin.ValueBool() {
		if err := r.client.Settings.Workspace.SetOpenJoin(ctx, plan.OpenJoin.ValueBool()); err != nil {
			return nil, err
		}
	}
	return r.read(ctx)
}

func (r *workspaceResource) read(ctx context.Context) (*workspaceModel, error) {
	w, err := r.client.Settings.Workspace.Get(ctx)
	if err != nil {
		return nil, err
	}
	var diags = new(diagsSink)
	m := &workspaceModel{
		ID:          types.StringValue(w.Slug),
		Name:        types.StringValue(w.Name),
		OpenJoin:    types.BoolValue(w.OpenJoin),
		Domains:     listOf(ctx, w.Domains, &diags.d),
		MailDomains: listOf(ctx, w.MailDomains, &diags.d),
	}
	return m, diags.err()
}

func (r *workspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m, err := r.apply(ctx, plan)
	if err != nil {
		failed(&resp.Diagnostics, "set the company", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *workspaceResource) Read(ctx context.Context, _ resource.ReadRequest, resp *resource.ReadResponse) {
	m, err := r.read(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "read the company", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *workspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m, err := r.apply(ctx, plan)
	if err != nil {
		failed(&resp.Diagnostics, "change the company", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *workspaceResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("The company stays",
		"yet_workspace only lets go of the company: its name and settings stay as they are.")
}

func (r *workspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ── yet_settings_apps ────────────────────────────────────────────────────────

type workspaceAppsResource struct{ base }

func newWorkspaceAppsResource() resource.Resource { return &workspaceAppsResource{} }

type workspaceAppsModel struct {
	ID  types.String `tfsdk:"id"`
	Off types.Set    `tfsdk:"off"`
}

func (r *workspaceAppsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_apps"
}

func (r *workspaceAppsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Which apps the company's people see. Every app is on but those listed in `off`; " +
			"an app Yet adds later comes on.\n\n" +
			"~> **Note:** Destroying this resource leaves the apps as they are.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The company's slug.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"off": schema.SetAttribute{
				MarkdownDescription: "The apps turned off: any of `" + joinTicks(yet.Apps) + "`.",
				ElementType:         types.StringType,
				Required:            true,
				Validators: []validator.Set{
					setvalidator.ValueStringsAre(stringvalidator.OneOf(yet.Apps...)),
				},
			},
		},
	}
}

func (r *workspaceAppsResource) set(ctx context.Context, plan workspaceAppsModel, m *workspaceAppsModel, d *diagsSink) error {
	off := stringsOf(ctx, plan.Off, &d.d)
	if d.d.HasError() {
		return d.err()
	}
	if err := r.client.Settings.Apps.SetOff(ctx, *off); err != nil {
		return err
	}
	return r.get(ctx, m, d)
}

func (r *workspaceAppsResource) get(ctx context.Context, m *workspaceAppsModel, d *diagsSink) error {
	off, err := r.client.Settings.Apps.Off(ctx)
	if err != nil {
		return err
	}
	slices.Sort(off)
	m.ID = types.StringValue(r.client.Workspace)
	m.Off = setOf(ctx, off, &d.d)
	return d.err()
}

func (r *workspaceAppsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, m workspaceAppsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	if err := r.set(ctx, plan, &m, d); err != nil {
		failed(&resp.Diagnostics, "set the company's apps", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *workspaceAppsResource) Read(ctx context.Context, _ resource.ReadRequest, resp *resource.ReadResponse) {
	var m workspaceAppsModel
	if err := r.get(ctx, &m, &diagsSink{}); err != nil {
		failed(&resp.Diagnostics, "read the company's apps", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *workspaceAppsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, m workspaceAppsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.set(ctx, plan, &m, &diagsSink{}); err != nil {
		failed(&resp.Diagnostics, "set the company's apps", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *workspaceAppsResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("The apps stay as they are",
		"yet_workspace_apps only lets go: the apps off stay off.")
}

func (r *workspaceAppsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
