package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

/*
A subdomain is for the mail a company's systems send — avisos.acme.com — each
with a reputation of its own, so a campaign's complaints never hold back an
order notice. Under a domain the company already showed is its own, it needs
no showing of its own: made, it is the company's at once, its mail on. Its
name is all there is to it, so a new name is a new subdomain.
*/

// ── yet_settings_mail_subdomain ──────────────────────────────────────────────

type subdomainResource struct{ base }

func newSubdomainResource() resource.Resource { return &subdomainResource{} }

type subdomainModel struct {
	ID     types.String `tfsdk:"id"`
	Name   types.String `tfsdk:"name"`
	Parent types.String `tfsdk:"parent"`
}

func (r *subdomainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_mail_subdomain"
}

func (r *subdomainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A subdomain of one of the company's domains, for the mail its systems send: on the mail server, " +
			"in SES, and its records in Cloud's zone of its parent when the zone is there. The API sends only from " +
			"a subdomain's addresses (`yet_settings_mail_send_email`), never from a domain its people's mailboxes are on.\n\n" +
			"!> **Warning:** Destroying it takes it from the mail server, its SES and the company, with its addresses.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The subdomain's name.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The whole name, lowercase, under one of the company's domains: `avisos.acme.com`.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          domainValidators,
			},
			"parent": schema.StringAttribute{
				MarkdownDescription: "The company's domain it is under: `acme.com`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// subdomain is one of the company's subdomains, by name, or nil.
func subdomain(ctx context.Context, c *yet.Client, name string) (*yet.Subdomain, error) {
	all, err := c.Settings.Subdomains.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range all {
		if d.Name == name {
			return &d, nil
		}
	}
	return nil, nil
}

func (r *subdomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan subdomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d, warnings, err := r.client.Settings.Subdomains.Add(ctx, plan.Name.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "add the subdomain", err)
		return
	}
	warned(&resp.Diagnostics, "turning its mail on", warnings)
	plan.ID = types.StringValue(d.Name)
	plan.Parent = types.StringValue(d.Parent)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *subdomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state subdomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d, err := subdomain(ctx, r.client, state.ID.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "read the subdomain", err)
		return
	}
	if d == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.Name = types.StringValue(d.Name)
	state.Parent = types.StringValue(d.Parent)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update never runs: the name replaces the subdomain.
func (r *subdomainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan subdomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *subdomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state subdomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Settings.Subdomains.Remove(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "remove the subdomain", err)
	}
}

func (r *subdomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
