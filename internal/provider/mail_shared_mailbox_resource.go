package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

/*
A shared mailbox is the company's, not a person's — pedidos@avisos.acme.com —
read by the people given it beside their own, in Mail under "Shared with
you". Its address is what it is, so a new address is a new mailbox (and the
old one goes with its messages); its name and its members change in place.
*/

// mailboxAddress is an address as the mail server keeps it: lowercase, so
// what the plan says is what comes back.
var mailboxAddress = regexp.MustCompile(`^[^@\sA-Z]+@[^@\sA-Z]+\.[^@\sA-Z]+$`)

// ── yet_settings_mail_shared_mailbox ─────────────────────────────────────────

type sharedMailboxResource struct{ base }

func newSharedMailboxResource() resource.Resource { return &sharedMailboxResource{} }

type sharedMailboxModel struct {
	ID      types.String `tfsdk:"id"`
	Address types.String `tfsdk:"address"`
	Name    types.String `tfsdk:"name"`
	Members types.Set    `tfsdk:"members"`
}

func (r *sharedMailboxResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_mail_shared_mailbox"
}

func (r *sharedMailboxResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	computed := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A shared mailbox: one of the company's, not of a person, that its members read beside " +
			"their own, in Mail under \"Shared with you\" — read, archived or labelled by one is so for all of them. " +
			"What the API sends from its address (`yet_settings_mail_send_email`) is kept in its Sent.\n\n" +
			"!> **Warning:** Destroying it, or changing its address, deletes it with every message in it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The shared mailbox's address.",
				Computed:            true,
				PlanModifiers:       computed,
			},
			"address": schema.StringAttribute{
				MarkdownDescription: "Its address, lowercase, at one of the company's domains with mail: `pedidos@avisos.acme.com`.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{stringvalidator.RegexMatches(mailboxAddress, "a lowercase email address")},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "What Mail calls it: `Pedidos`. The address before the @ when not given.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       computed,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 100)},
			},
			"members": schema.SetAttribute{
				MarkdownDescription: "The people in it, by address — only the company's — whole: those left out stop seeing it.",
				ElementType:         types.StringType,
				Required:            true,
				Validators: []validator.Set{setvalidator.ValueStringsAre(
					stringvalidator.RegexMatches(mailboxAddress, "a lowercase email address"),
				)},
			},
		},
	}
}

func (r *sharedMailboxResource) fill(ctx context.Context, m *sharedMailboxModel, s *yet.SharedMailbox, d *diagsSink) {
	m.ID = types.StringValue(s.Address)
	m.Address = types.StringValue(s.Address)
	m.Name = types.StringValue(s.Name)
	m.Members = setOf(ctx, s.Members, &d.d)
}

// sharedMailbox is one of the company's shared mailboxes, by address, or nil.
func sharedMailbox(ctx context.Context, c *yet.Client, address string) (*yet.SharedMailbox, error) {
	all, err := c.Settings.SharedMailboxes.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range all {
		if m.Address == address {
			return &m, nil
		}
	}
	return nil, nil
}

func (r *sharedMailboxResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sharedMailboxModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	members := stringsOf(ctx, plan.Members, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.Settings.SharedMailboxes.Create(ctx, yet.NewSharedMailbox{
		Address: plan.Address.ValueString(),
		Name:    plan.Name.ValueString(),
		Members: *members,
	})
	if err != nil {
		failed(&resp.Diagnostics, "make the shared mailbox", err)
		return
	}
	d := &diagsSink{}
	r.fill(ctx, &plan, s, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *sharedMailboxResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sharedMailboxModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := sharedMailbox(ctx, r.client, state.ID.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "read the shared mailbox", err)
		return
	}
	if s == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	d := &diagsSink{}
	r.fill(ctx, &state, s, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *sharedMailboxResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state sharedMailboxModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	members := stringsOf(ctx, plan.Members, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	change := yet.SharedMailboxChange{Members: *members}
	if !plan.Name.IsUnknown() && !plan.Name.Equal(state.Name) {
		change.Name = plan.Name.ValueStringPointer()
	}
	s, err := r.client.Settings.SharedMailboxes.Update(ctx, state.ID.ValueString(), change)
	if err != nil {
		failed(&resp.Diagnostics, "change the shared mailbox", err)
		return
	}
	d := &diagsSink{}
	r.fill(ctx, &plan, s, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *sharedMailboxResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sharedMailboxModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Settings.SharedMailboxes.Delete(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the shared mailbox", err)
	}
}

func (r *sharedMailboxResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
