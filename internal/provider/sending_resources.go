package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
How the company's mail leaves and where Drive keeps its files: in its own AWS
accounts (yet_settings_aws_account), when it brings them. Each is one per
company, so these resources are singletons, their id the company's slug. The
choice of SES stays when yet_settings_sending is destroyed; the bucket has a
way back in Settings, and destroying it takes it.
*/

// warned adds what yet did, but not quite, as warnings: the change itself happened.
func warned(diags *diag.Diagnostics, doing string, warnings []string) {
	for _, w := range warnings {
		diags.AddWarning("While "+doing, w)
	}
}

// ── yet_settings_sending ─────────────────────────────────────────────────────

type sendingResource struct{ base }

func newSendingResource() resource.Resource { return &sendingResource{} }

type sendingModel struct {
	ID         types.String `tfsdk:"id"`
	Sending    types.String `tfsdk:"sending"`
	SESAllowed types.Bool   `tfsdk:"ses_allowed"`
}

func (r *sendingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_sending"
}

func (r *sendingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "How the company's mail leaves: through Yet's SES, or the SES of its own AWS account " +
			"(`yet_settings_connection_aws`). Switching moves every domain with mail to the other SES.\n\n" +
			"~> **Note:** There is one per company: creating this resource takes hold of it, and destroying it leaves the choice as it is.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The company's slug.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"sending": schema.StringAttribute{
				MarkdownDescription: "`yet`, through Yet's SES — when Yet opens it to the company (`ses_allowed`) — or `own`.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.OneOf("yet", "own")},
			},
			"ses_allowed": schema.BoolAttribute{
				MarkdownDescription: "Whether Yet's SES is open to the company at all.",
				Computed:            true,
			},
		},
	}
}

func (r *sendingResource) read(ctx context.Context) (*sendingModel, error) {
	s, err := r.client.Settings.Sending.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &sendingModel{
		ID:         types.StringValue(r.client.Workspace),
		Sending:    types.StringValue(s.Sending),
		SESAllowed: types.BoolValue(s.SESAllowed),
	}, nil
}

func (r *sendingResource) apply(ctx context.Context, plan sendingModel, diags *diag.Diagnostics) *sendingModel {
	now, err := r.read(ctx)
	if err != nil {
		failed(diags, "read how mail leaves", err)
		return nil
	}
	if now.Sending.ValueString() != plan.Sending.ValueString() {
		warnings, err := r.client.Settings.Sending.Set(ctx, plan.Sending.ValueString())
		if err != nil {
			failed(diags, "choose how mail leaves", err)
			return nil
		}
		warned(diags, "moving the domains", warnings)
		if now, err = r.read(ctx); err != nil {
			failed(diags, "read how mail leaves", err)
			return nil
		}
	}
	return now
}

func (r *sendingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sendingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m := r.apply(ctx, plan, &resp.Diagnostics); m != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
}

func (r *sendingResource) Read(ctx context.Context, _ resource.ReadRequest, resp *resource.ReadResponse) {
	m, err := r.read(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "read how mail leaves", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *sendingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan sendingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m := r.apply(ctx, plan, &resp.Diagnostics); m != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
}

func (r *sendingResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("Mail leaves as it does",
		"yet_settings_sending only lets go: the company goes on sending through the SES it chose.")
}

func (r *sendingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ── yet_settings_storage ─────────────────────────────────────────────────────

type storageResource struct{ base }

func newStorageResource() resource.Resource { return &storageResource{} }

type storageModel struct {
	ID     types.String `tfsdk:"id"`
	Bucket types.String `tfsdk:"bucket"`
	Prefix types.String `tfsdk:"prefix"`
}

var (
	s3Bucket = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	s3Prefix = regexp.MustCompile(`^([^/\s](.*[^/\s])?)?$`)
)

func (r *storageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_storage"
}

func (r *storageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Where Drive keeps the company's files: a bucket in its own AWS account, reached with " +
			"`yet_settings_connection_aws`'s role. Yet writes to it once, and removes what it wrote, to be sure the role may use it — the stack makes one, set when the account connects; another of the company's goes in the stack as its `ExistingDriveBucket`.\n\n" +
			"~> **Note:** Destroying it sends Drive back to keeping the files itself. The bucket, and what is in it, stay in the company's account.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The company's slug.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"bucket": schema.StringAttribute{
				MarkdownDescription: "The S3 bucket's name.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(s3Bucket, "an S3 bucket name")},
			},
			"prefix": schema.StringAttribute{
				MarkdownDescription: "A folder inside the bucket, so it can be shared with other things: `yet/drive`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				// As Yet keeps it, so the state says what was given.
				Validators: []validator.String{stringvalidator.RegexMatches(s3Prefix, "no slash or space at either end")},
			},
		},
	}
}

func (r *storageResource) set(ctx context.Context, plan storageModel) (*storageModel, error) {
	if err := r.client.Settings.Storage.Set(ctx, yet.Storage{Bucket: plan.Bucket.ValueString(), Prefix: plan.Prefix.ValueString()}); err != nil {
		return nil, err
	}
	plan.ID = types.StringValue(r.client.Workspace)
	return &plan, nil
}

func (r *storageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan storageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m, err := r.set(ctx, plan)
	if err != nil {
		failed(&resp.Diagnostics, "set Drive's bucket", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *storageResource) Read(ctx context.Context, _ resource.ReadRequest, resp *resource.ReadResponse) {
	st, err := r.client.Settings.Storage.Get(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "read Drive's bucket", err)
		return
	}
	if st == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, storageModel{
		ID:     types.StringValue(r.client.Workspace),
		Bucket: types.StringValue(st.Bucket),
		Prefix: types.StringValue(st.Prefix),
	})...)
}

func (r *storageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan storageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m, err := r.set(ctx, plan)
	if err != nil {
		failed(&resp.Diagnostics, "set Drive's bucket", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *storageResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	if err := r.client.Settings.Storage.Remove(ctx); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "let go of Drive's bucket", err)
	}
}

func (r *storageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
