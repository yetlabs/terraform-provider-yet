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
The addresses of a domain with mail that are not a person — an alias, a
list, the catch-all — each its own resource, as Settings → Mail → Addresses
has them. One namespace: an address is a person's, an alias or a list.
*/

// localName is the part of an address before the @, as the mail server takes it.
var localName = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,62}[a-z0-9])?$`)

var emailAddress = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

const localNameRule = "lowercase letters, digits, dots, hyphens and underscores, starting and ending with a letter or digit"

// ── yet_settings_mail_alias ──────────────────────────────────────────────────

type mailAliasResource struct{ base }

func newMailAliasResource() resource.Resource { return &mailAliasResource{} }

type mailAliasModel struct {
	ID       types.String `tfsdk:"id"`
	PersonID types.String `tfsdk:"person_id"`
	Name     types.String `tfsdk:"name"`
	Domain   types.String `tfsdk:"domain"`
	Address  types.String `tfsdk:"address"`
}

func (r *mailAliasResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_mail_alias"
}

func (r *mailAliasResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	computed := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Another address for someone of the company: `contato@` landing in Ana's inbox. " +
			"It is at the person's own domain.\n\n" +
			"-> **Note:** Mail to the alias goes on arriving in the person's mailbox until it is destroyed; destroying it bounces what comes after.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "`<person_id>/<name>`.",
				Computed:            true,
				PlanModifiers:       computed,
			},
			"person_id": schema.StringAttribute{
				MarkdownDescription: "Whose inbox it lands in: a `yet_settings_person`'s `id`.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The alias before the @: `contato`.",
				Required:            true,
				PlanModifiers:       replace,
				Validators:          []validator.String{stringvalidator.RegexMatches(localName, localNameRule)},
			},
			"domain": schema.StringAttribute{
				MarkdownDescription: "The person's domain, where the alias is.",
				Computed:            true,
				PlanModifiers:       computed,
			},
			"address": schema.StringAttribute{
				MarkdownDescription: "The whole alias: `contato@acme.example.org`.",
				Computed:            true,
				PlanModifiers:       computed,
			},
		},
	}
}

// find is the alias as the mail server has it, or a not found.
func (r *mailAliasResource) find(ctx context.Context, personID, name string) (*mailAliasModel, error) {
	p, err := r.client.Settings.People.Get(ctx, personID)
	if err != nil {
		return nil, err
	}
	_, domain, _ := strings.Cut(p.Email, "@")
	a, err := r.client.Settings.Addresses.Get(ctx, domain)
	if err != nil {
		return nil, err
	}
	for _, alias := range a.Aliases {
		if alias.AccountID == personID && alias.Name == name {
			return &mailAliasModel{
				ID:       types.StringValue(personID + "/" + name),
				PersonID: types.StringValue(personID),
				Name:     types.StringValue(name),
				Domain:   types.StringValue(domain),
				Address:  types.StringValue(alias.Address),
			}, nil
		}
	}
	return nil, &yet.Error{StatusCode: 404, Method: "GET", Path: "/v1/mail/domains/" + domain + "/addresses", Message: name + " is not one of " + p.Email + "'s aliases."}
}

func (r *mailAliasResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mailAliasModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	personID, name := plan.PersonID.ValueString(), plan.Name.ValueString()
	p, err := r.client.Settings.People.Get(ctx, personID)
	if err != nil {
		failed(&resp.Diagnostics, "find the person", err)
		return
	}
	_, domain, _ := strings.Cut(p.Email, "@")
	if err := r.client.Settings.Addresses.AddAlias(ctx, domain, personID, name); err != nil {
		failed(&resp.Diagnostics, "add the alias", err)
		return
	}
	m, err := r.find(ctx, personID, name)
	if err != nil {
		failed(&resp.Diagnostics, "read the alias", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *mailAliasResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mailAliasModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	personID, name := state.PersonID.ValueString(), state.Name.ValueString()
	// An import has only the id.
	if personID == "" {
		personID, name, _ = strings.Cut(state.ID.ValueString(), "/")
	}
	m, err := r.find(ctx, personID, name)
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the alias", err)
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// Update never runs: every attribute given replaces the alias.
func (r *mailAliasResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan mailAliasModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *mailAliasResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state mailAliasModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Settings.Addresses.RemoveAlias(ctx, state.Domain.ValueString(), state.PersonID.ValueString(), state.Name.ValueString())
	if err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "remove the alias", err)
	}
}

func (r *mailAliasResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if personID, name, ok := strings.Cut(req.ID, "/"); !ok || personID == "" || name == "" {
		resp.Diagnostics.AddError("Invalid import ID", "An alias is imported as <person_id>/<name>: 3c/contato.")
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ── yet_settings_mail_list ───────────────────────────────────────────────────

type mailListResource struct{ base }

func newMailListResource() resource.Resource { return &mailListResource{} }

type mailListModel struct {
	ID          types.String `tfsdk:"id"`
	Domain      types.String `tfsdk:"domain"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Recipients  types.Set    `tfsdk:"recipients"`
	Address     types.String `tfsdk:"address"`
}

func (r *mailListResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_mail_list"
}

func (r *mailListResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	computed := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A list: one address of the company's that delivers to several — " +
			"its own people, or anyone outside.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The list's id, as the mail server has it.",
				Computed:            true,
				PlanModifiers:       computed,
			},
			"domain": schema.StringAttribute{
				MarkdownDescription: "One of the company's domains with mail.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The list before the @: `financeiro`.",
				Required:            true,
				PlanModifiers:       replace,
				Validators:          []validator.String{stringvalidator.RegexMatches(localName, localNameRule)},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "What the list is for, as Settings shows it.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
			},
			"recipients": schema.SetAttribute{
				MarkdownDescription: "The addresses it delivers to, whole: those left out stop receiving it.",
				ElementType:         types.StringType,
				Required:            true,
			},
			"address": schema.StringAttribute{
				MarkdownDescription: "The list's address: `financeiro@acme.example.org`.",
				Computed:            true,
				PlanModifiers:       computed,
			},
		},
	}
}

func (r *mailListResource) fill(ctx context.Context, m *mailListModel, l *yet.MailList, d *diagsSink) {
	m.ID = types.StringValue(l.ID)
	m.Name = types.StringValue(l.Name)
	m.Address = types.StringValue(l.Address)
	_, domain, _ := strings.Cut(l.Address, "@")
	m.Domain = types.StringValue(domain)
	m.Description = types.StringValue(l.Description)
	m.Recipients = setOf(ctx, l.Recipients, &d.d)
}

func (r *mailListResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mailListModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	recipients := stringsOf(ctx, plan.Recipients, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	l, err := r.client.Settings.Addresses.CreateList(ctx, plan.Domain.ValueString(), yet.NewList{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
		Recipients:  *recipients,
	})
	if err != nil {
		failed(&resp.Diagnostics, "make the list", err)
		return
	}
	d := &diagsSink{}
	r.fill(ctx, &plan, l, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *mailListResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mailListModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	l, err := r.client.Settings.Addresses.GetList(ctx, state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the list", err)
		}
		return
	}
	d := &diagsSink{}
	r.fill(ctx, &state, l, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *mailListResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state mailListModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	recipients := stringsOf(ctx, plan.Recipients, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	description := plan.Description.ValueString()
	if err := r.client.Settings.Addresses.UpdateList(ctx, id, yet.ListChange{Description: &description, Recipients: *recipients}); err != nil {
		failed(&resp.Diagnostics, "change the list", err)
		return
	}
	l, err := r.client.Settings.Addresses.GetList(ctx, id)
	if err != nil {
		failed(&resp.Diagnostics, "read the list", err)
		return
	}
	d := &diagsSink{}
	r.fill(ctx, &plan, l, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *mailListResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state mailListModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Settings.Addresses.DeleteList(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the list", err)
	}
}

func (r *mailListResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ── yet_settings_mail_catch_all ──────────────────────────────────────────────

type mailCatchAllResource struct{ base }

func newMailCatchAllResource() resource.Resource { return &mailCatchAllResource{} }

type mailCatchAllModel struct {
	ID      types.String `tfsdk:"id"`
	Domain  types.String `tfsdk:"domain"`
	Address types.String `tfsdk:"address"`
}

func (r *mailCatchAllResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_mail_catch_all"
}

func (r *mailCatchAllResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Where mail to a domain's addresses that do not exist goes, instead of bouncing.\n\n" +
			"-> **Note:** Destroying it turns the catch-all off: such mail bounces again.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The domain.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"domain": schema.StringAttribute{
				MarkdownDescription: "One of the company's domains with mail.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"address": schema.StringAttribute{
				MarkdownDescription: "The address it all goes to — a person, a list, or anyone outside.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(emailAddress, "an email address")},
			},
		},
	}
}

func (r *mailCatchAllResource) apply(ctx context.Context, plan mailCatchAllModel) (*mailCatchAllModel, error) {
	domain := plan.Domain.ValueString()
	if err := r.client.Settings.Addresses.SetCatchAll(ctx, domain, plan.Address.ValueString()); err != nil {
		return nil, err
	}
	return r.read(ctx, domain)
}

func (r *mailCatchAllResource) read(ctx context.Context, domain string) (*mailCatchAllModel, error) {
	a, err := r.client.Settings.Addresses.Get(ctx, domain)
	if err != nil {
		return nil, err
	}
	if a.CatchAll == "" {
		return nil, &yet.Error{StatusCode: 404, Method: "GET", Path: "/v1/mail/domains/" + domain + "/addresses", Message: domain + " has no catch-all."}
	}
	return &mailCatchAllModel{
		ID:      types.StringValue(domain),
		Domain:  types.StringValue(domain),
		Address: types.StringValue(a.CatchAll),
	}, nil
}

func (r *mailCatchAllResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mailCatchAllModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m, err := r.apply(ctx, plan)
	if err != nil {
		failed(&resp.Diagnostics, "set the catch-all", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *mailCatchAllResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mailCatchAllModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m, err := r.read(ctx, state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the catch-all", err)
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *mailCatchAllResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan mailCatchAllModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m, err := r.apply(ctx, plan)
	if err != nil {
		failed(&resp.Diagnostics, "set the catch-all", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *mailCatchAllResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state mailCatchAllModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Settings.Addresses.SetCatchAll(ctx, state.Domain.ValueString(), ""); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "turn the catch-all off", err)
	}
}

func (r *mailCatchAllResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
