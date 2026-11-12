package provider

import (
	"context"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
A company adds its own domains in two steps, as Settings → Domains does: it
asks for one, and gets a TXT record to publish at its root; once DNS shows
it — or the domain's zone in the company's Cloud answers — the domain is the
company's. The ask is yet_settings_domain_claim, whose txt goes into any DNS
resource; the domain is yet_settings_domain, which waits for DNS to show it.
Mail is turned on apart, and is the domain's to keep.
*/

// domainName is a domain as Settings takes it; its length, 4 to 253, is checked apart.
var domainName = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

var domainValidators = []validator.String{
	stringvalidator.LengthBetween(4, 253),
	stringvalidator.RegexMatches(domainName, "a lowercase domain name"),
}

// Each try to verify a domain, and for how long: DNS takes its time.
var (
	verifyEvery = 15 * time.Second
	verifyFor   = 10 * time.Minute
)

// domainState is a domain as the company's Settings have it: its own, asked for, or neither.
func domainState(ctx context.Context, c *yet.Client, name string) (*yet.Domain, *yet.DomainClaim, error) {
	domains, claims, err := c.Settings.Domains.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range domains {
		if d.Name == name {
			return &d, nil, nil
		}
	}
	for _, x := range claims {
		if x.Domain == name {
			return nil, &x, nil
		}
	}
	return nil, nil, nil
}

// ── yet_settings_domain_claim ────────────────────────────────────────────────

type domainClaimResource struct{ base }

func newDomainClaimResource() resource.Resource { return &domainClaimResource{} }

type domainClaimModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	TXT  types.String `tfsdk:"txt"`
}

func (r *domainClaimResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_domain_claim"
}

func (r *domainClaimResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A domain the company asks for. It is the company's once a TXT record at its root says `txt` — " +
			"publish it with any DNS resource — and `yet_settings_domain` verifies it.\n\n" +
			"-> **Note:** A domain another company asked for and has not shown in a week can be asked for again. " +
			"Once the domain is the company's, the claim has done its work: destroying it then changes nothing.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The domain.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The domain, lowercase: `acme.dev`.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          domainValidators,
			},
			"txt": schema.StringAttribute{
				MarkdownDescription: "What the TXT record at the domain's root must say: `yet-verification=…`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *domainClaimResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainClaimModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Name.ValueString()
	plan.ID = types.StringValue(name)
	claim, err := r.client.Settings.Domains.Claim(ctx, name)
	if err != nil {
		// The company's already: nothing to ask for, and nothing to publish.
		if own, _, e := domainState(ctx, r.client, name); yet.IsConflict(err) && e == nil && own != nil {
			plan.TXT = types.StringValue("")
			resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
			return
		}
		failed(&resp.Diagnostics, "ask for the domain", err)
		return
	}
	plan.TXT = types.StringValue(claim.TXT)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *domainClaimResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainClaimModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := state.ID.ValueString()
	own, claim, err := domainState(ctx, r.client, name)
	switch {
	case err != nil:
		failed(&resp.Diagnostics, "read the domain", err)
		return
	case claim != nil:
		state.TXT = types.StringValue(claim.TXT)
	case own != nil:
		// Shown and the company's: the claim stays as it was, its work done.
		if state.TXT.IsNull() {
			state.TXT = types.StringValue("")
		}
	default:
		resp.State.RemoveResource(ctx)
		return
	}
	state.Name = types.StringValue(name)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update never runs: the name replaces the claim.
func (r *domainClaimResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan domainClaimModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *domainClaimResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state domainClaimModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Settings.Domains.CancelClaim(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "stop asking for the domain", err)
	}
}

func (r *domainClaimResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ── yet_settings_domain ──────────────────────────────────────────────────────

type domainResource struct{ base }

func newDomainResource() resource.Resource { return &domainResource{} }

var _ resource.ResourceWithModifyPlan = (*domainResource)(nil)

type domainModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Mail types.Bool   `tfsdk:"mail"`
	How  types.String `tfsdk:"verified_by"`
}

func (r *domainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_domain"
}

func (r *domainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One of the company's domains: whoever has an address at it may sign in, and with `mail`, " +
			"its mail is here — mailboxes, lists, sending.\n\n" +
			"-> **Note:** A domain becomes the company's once its DNS shows it: the TXT record of its `yet_settings_domain_claim` at its root, " +
			"or its zone in the company's Cloud answering. Creating this resource checks every 15 seconds for up to 10 minutes; " +
			"a domain the company has already is taken hold of.\n\n" +
			"~> **Note:** Mail, once on, stays on: `mail` cannot go back to `false`. Without mail, destroying the domain leaves it the company's — only Yet's operator lets such a domain go.\n\n" +
			"!> **Warning:** With mail, destroying the domain takes it off the mail server and out of the company. Yet refuses while anyone has a mailbox at it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The domain.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The domain, lowercase: `acme.dev`.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          domainValidators,
			},
			"mail": schema.BoolAttribute{
				MarkdownDescription: "Whether its mail is here: the domain made on the mail server and in the company's SES, " +
					"a mailbox for each of its people already in. Its MX must point here for mail to arrive.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"verified_by": schema.StringAttribute{
				MarkdownDescription: "What showed the domain the company's: `txt` or `cloud` — empty when it was the company's already.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// ModifyPlan refuses, at plan, what Settings cannot do: turn a domain's mail off.
func (r *domainResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state domainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.Mail.ValueBool() && known(plan.Mail) && !plan.Mail.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("mail"), "Mail stays on",
			state.Name.ValueString()+" has mail here, and Settings does not turn a domain's mail off.")
	}
}

// verify waits for the domain's DNS to show it the company's, as long as Terraform lets it.
func (r *domainResource) verify(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, verifyFor)
	defer cancel()
	for {
		how, err := r.client.Settings.Domains.Verify(ctx, name)
		if err == nil || !yet.IsConflict(err) {
			return how, err
		}
		select {
		case <-ctx.Done():
			return "", err
		case <-time.After(verifyEvery):
		}
	}
}

func (r *domainResource) enableMail(ctx context.Context, name string, diags *diag.Diagnostics) {
	warnings, err := r.client.Settings.Domains.EnableMail(ctx, name)
	if err != nil {
		failed(diags, "turn the domain's mail on", err)
		return
	}
	warned(diags, "turning the domain's mail on", warnings)
}

func (r *domainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Name.ValueString()
	own, _, err := domainState(ctx, r.client, name)
	if err != nil {
		failed(&resp.Diagnostics, "read the company's domains", err)
		return
	}
	if own != nil && own.Mail && !plan.Mail.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("mail"), "Mail is on",
			name+" has mail here already, and it stays on: set mail = true.")
		return
	}
	plan.ID = types.StringValue(name)
	plan.How = types.StringValue("")
	if own == nil {
		how, err := r.verify(ctx, name)
		if err != nil {
			failed(&resp.Diagnostics, "verify the domain", err)
			return
		}
		plan.How = types.StringValue(how)
		own = &yet.Domain{Name: name}
	}
	if plan.Mail.ValueBool() && !own.Mail {
		// The domain is the company's from here, whatever mail says: kept, so the next apply goes on.
		if r.enableMail(ctx, name, &resp.Diagnostics); resp.Diagnostics.HasError() {
			plan.Mail = types.BoolValue(false)
			resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *domainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := state.ID.ValueString()
	own, _, err := domainState(ctx, r.client, name)
	if err != nil {
		failed(&resp.Diagnostics, "read the domain", err)
		return
	}
	if own == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.Name = types.StringValue(name)
	state.Mail = types.BoolValue(own.Mail)
	if state.How.IsNull() {
		state.How = types.StringValue("")
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *domainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state domainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Mail.ValueBool() && !state.Mail.ValueBool() {
		if r.enableMail(ctx, state.ID.ValueString(), &resp.Diagnostics); resp.Diagnostics.HasError() {
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *domainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state domainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !state.Mail.ValueBool() {
		resp.Diagnostics.AddWarning("The domain stays the company's",
			state.ID.ValueString()+" has no mail here: Settings does not let it go, Yet's operator does.")
		return
	}
	if err := r.client.Settings.Domains.Remove(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "remove the domain", err)
	}
}

func (r *domainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
