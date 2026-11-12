package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

/*
yet_person is someone with a mailbox here, given a password: what code can
make whole. An invitation waits for a person to open it, so it is the
apps' — and Settings' — to send. What hands out power stays theirs too:
access to Settings given, and a password set again once they have one — a
key is refused both, so admin is only ever read or taken away here.
*/

type personResource struct{ base }

func newPersonResource() resource.Resource { return &personResource{} }

type personModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Domain   types.String `tfsdk:"domain"`
	Aliases  types.List   `tfsdk:"aliases"`
	FullName types.String `tfsdk:"full_name"`
	Password types.String `tfsdk:"password"`
	Admin    types.Bool   `tfsdk:"admin"`
	Email    types.String `tfsdk:"email"`
}

func (r *personResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_person"
}

func (r *personResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Someone of the company with a mailbox here, who signs in with it.\n\n" +
			"!> **Warning:** Destroying it removes their sign-in and their mailbox, with every message in it. It cannot be undone.\n\n" +
			"-> **Note:** An invitation waits for a person to open it, so it stays in Yet's apps: here, people are given a password.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Their mailbox's name, before the @: `ana`.",
				Required:            true,
				PlanModifiers:       replace,
				Validators: []validator.String{
					stringvalidator.RegexMatches(nameChars, "letters, digits, dots, hyphens and underscores"),
				},
			},
			"domain": schema.StringAttribute{
				MarkdownDescription: "One of the company's domains with mail: their address is `name@domain`.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"aliases": schema.ListAttribute{
				MarkdownDescription: "Other domains of the company with mail where `name@` reaches them too.",
				ElementType:         types.StringType,
				Optional:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.RequiresReplace()},
			},
			"full_name": schema.StringAttribute{
				MarkdownDescription: "Their name, as the apps and their mail show it.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "Their password when they are added, at least 8 characters. Setting it again takes a person signed in to Settings — an API key never sets someone else's — so changing it here is refused; imported, it is taken as given.",
				Required:            true,
				Sensitive:           true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(8)},
			},
			"admin": schema.BoolAttribute{
				MarkdownDescription: "Whether they may open the company's Settings. Only a person signed in to Settings gives it — `true` with an API key is refused — so here it is read, or set to `false` to take it away; left out, it stays as Settings has it.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "The address they sign in with.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *personResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan personModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domains := []string{plan.Domain.ValueString()}
	if aliases := stringsOf(ctx, plan.Aliases, &resp.Diagnostics); aliases != nil {
		domains = append(domains, *aliases...)
	}
	email, err := r.client.Settings.People.Add(ctx, yet.NewPerson{
		FullName:     plan.FullName.ValueString(),
		Via:          "password",
		Password:     plan.Password.ValueString(),
		Mailbox:      &yet.Mailbox{Name: plan.Name.ValueString(), Domains: domains},
		RunsSettings: plan.Admin.ValueBool(),
	})
	if err != nil {
		failed(&resp.Diagnostics, "add the person", err)
		return
	}
	people, _, err := r.client.Settings.People.List(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "read the person", err)
		return
	}
	for _, p := range people {
		if strings.EqualFold(p.Email, email) {
			fill(&plan, p)
			resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
			return
		}
	}
	resp.Diagnostics.AddError("Could not find the person", email+" was added, but is not among the company's people.")
}

func fill(m *personModel, p yet.Person) {
	m.ID = types.StringValue(p.ID)
	m.Email = types.StringValue(p.Email)
	m.FullName = types.StringValue(p.FullName)
	m.Admin = types.BoolValue(p.RunsSettings)
	name, domain, _ := strings.Cut(p.Email, "@")
	m.Name = types.StringValue(name)
	m.Domain = types.StringValue(domain)
}

func (r *personResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state personModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.client.Settings.People.Get(ctx, state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the person", err)
		}
		return
	}
	fill(&state, *p)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *personResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state personModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	people := r.client.Settings.People
	if !plan.FullName.Equal(state.FullName) {
		if err := people.Rename(ctx, id, plan.FullName.ValueString()); err != nil {
			failed(&resp.Diagnostics, "rename the person", err)
			return
		}
	}
	if !plan.Admin.IsUnknown() && !plan.Admin.Equal(state.Admin) {
		if err := people.SetRunsSettings(ctx, id, plan.Admin.ValueBool()); err != nil {
			failed(&resp.Diagnostics, "change who opens Settings", err)
			return
		}
	}
	// Imported, the password is not known: the one given is taken as theirs.
	if !state.Password.IsNull() && !plan.Password.Equal(state.Password) {
		resp.Diagnostics.AddAttributeError(path.Root("password"), "The password is set again in Settings",
			"Someone else's password is set by a person signed in to Yet's Settings, never with an API key. Put the one they were added with back here.")
		return
	}
	p, err := people.Get(ctx, id)
	if err != nil {
		failed(&resp.Diagnostics, "read the person", err)
		return
	}
	fill(&plan, *p)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *personResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state personModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Settings.People.Remove(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "remove the person", err)
	}
}

func (r *personResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
