package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
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
The company's AWS accounts, each connected by the role Yet's stack makes in
it. An account is added first — named, in a region, for what it is for —
which gives it the external ID its stack is made with; the stack is made
(aws_cloudformation_stack, with yet_settings_aws_launch); then its role is
handed to Yet (yet_settings_aws_account_role). Two resources, because the
stack sits between them.
*/

var (
	awsRegion = regexp.MustCompile(`^[a-z]{2}(-gov)?-[a-z]+-\d$`)
	awsRole   = regexp.MustCompile(`^arn:aws:iam::\d{12}:role/[\w+=,.@/-]+$`)
)

// ── yet_settings_aws_account ─────────────────────────────────────────────────

type awsAccountResource struct{ base }

func newAWSAccountResource() resource.Resource { return &awsAccountResource{} }

type awsAccountModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	Region     types.String `tfsdk:"region"`
	Uses       types.Set    `tfsdk:"uses"`
	Prefix     types.String `tfsdk:"bucket_prefix"`
	ExternalID types.String `tfsdk:"external_id"`
}

func (r *awsAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_aws_account"
}

func (r *awsAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One of the company's AWS accounts, for some of Yet's products: Mail's own SES, Drive's documents, Cloud's databases' copies — " +
			"each of those one account's. Its `external_id` is what its stack is made with; `yet_settings_aws_account_role` connects it.\n\n" +
			"!> **Warning:** Destroying it forgets the account: what it was for stops. Its stack stays in AWS.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Its id in Yet.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Its name, as Settings shows it: `Production`.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 40)},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Where its stack runs, and its SES sends from: `us-east-1`.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(awsRegion, "an AWS region")},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"uses": schema.SetAttribute{
				MarkdownDescription: "What it is for: `mail`, `drive`, `databases`, `vault` — connected, it takes those from any other account — " +
					"and `servers`, Cloud's EC2 instances, and `applications`, its applications' S3 buckets, which any account may be for.",
				ElementType: types.StringType,
				Required:    true,
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.OneOf("mail", "drive", "databases", "servers", "applications", "vault")),
				},
			},
			"bucket_prefix": schema.StringAttribute{
				MarkdownDescription: "What its applications' buckets begin with — its stack's `ApplicationsBucketPrefix`, " +
					"set in the stack too: empty, `yet-<company>-`; `*` any name, which lets Yet set up any bucket of the account.",
				Optional: true, Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators: []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^([a-z0-9][a-z0-9.-]{0,39}|\*)?$`),
					"lowercase letters, digits, dots and hyphens, up to 40 — or *")},
			},
			"external_id": schema.StringAttribute{
				MarkdownDescription: "Its external ID: its stack's `ExternalId`.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (m *awsAccountModel) from(ctx context.Context, a *yet.AWSAccount, diags *diagsSink) {
	m.ID = types.StringValue(a.ID)
	m.Name = types.StringValue(a.Name)
	m.Region = types.StringValue(a.Region)
	m.Uses = setOf(ctx, a.Uses, &diags.d)
	m.Prefix = types.StringValue(a.BucketPrefix)
	m.ExternalID = types.StringValue(a.ExternalID)
}

func (r *awsAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan awsAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var uses []string
	resp.Diagnostics.Append(plan.Uses.ElementsAs(ctx, &uses, false)...)
	a, err := r.client.Settings.Connections.AWS.Add(ctx, yet.AWSAccountInput{
		Name: plan.Name.ValueString(), Region: plan.Region.ValueString(), Uses: uses,
	})
	if err != nil {
		failed(&resp.Diagnostics, "add the AWS account", err)
		return
	}
	if p := plan.Prefix; !p.IsNull() && !p.IsUnknown() && p.ValueString() != "" {
		if a, err = r.client.Settings.Connections.AWS.SetBucketPrefix(ctx, a.ID, p.ValueString()); err != nil {
			failed(&resp.Diagnostics, "set the AWS account's buckets' beginning", err)
			return
		}
	}
	d := &diagsSink{}
	plan.from(ctx, a, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *awsAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state awsAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	all, err := r.client.Settings.Connections.AWS.List(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "read the AWS accounts", err)
		return
	}
	for _, a := range all {
		if a.ID == state.ID.ValueString() {
			d := &diagsSink{}
			state.from(ctx, &a, d)
			resp.Diagnostics.Append(d.d...)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *awsAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan awsAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var uses []string
	resp.Diagnostics.Append(plan.Uses.ElementsAs(ctx, &uses, false)...)
	a, err := r.client.Settings.Connections.AWS.Update(ctx, plan.ID.ValueString(), plan.Name.ValueString(), uses)
	if err != nil {
		failed(&resp.Diagnostics, "change the AWS account", err)
		return
	}
	if p := plan.Prefix; !p.IsNull() && !p.IsUnknown() && p.ValueString() != a.BucketPrefix {
		if a, err = r.client.Settings.Connections.AWS.SetBucketPrefix(ctx, a.ID, p.ValueString()); err != nil {
			failed(&resp.Diagnostics, "set the AWS account's buckets' beginning", err)
			return
		}
	}
	d := &diagsSink{}
	plan.from(ctx, a, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *awsAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state awsAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if err := r.client.Settings.Connections.AWS.Remove(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "forget the AWS account", err)
	}
}

func (r *awsAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ── yet_settings_aws_account_role ────────────────────────────────────────────

type awsRoleResource struct{ base }

func newAWSAccountRoleResource() resource.Resource { return &awsRoleResource{} }

type awsRoleModel struct {
	AccountID types.String `tfsdk:"account_id"`
	RoleARN   types.String `tfsdk:"role_arn"`
}

func (r *awsRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_aws_account_role"
}

func (r *awsRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The role an AWS account's stack made, handed to Yet: assumed and checked with AWS before it is kept. " +
			"Connected, the account takes what it is for from any other.\n\n" +
			"-> **Note:** Destroying it leaves the account connected; destroying `yet_settings_aws_account` forgets it.",
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				MarkdownDescription: "The account in Yet: `yet_settings_aws_account`'s `id`.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"role_arn": schema.StringAttribute{
				MarkdownDescription: "The role its stack made: its `RoleArn` output.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(awsRole, "a role's ARN (arn:aws:iam::…:role/…)")},
			},
		},
	}
}

func (r *awsRoleResource) connect(ctx context.Context, m awsRoleModel, resp *diagsSink) {
	warnings, err := r.client.Settings.Connections.AWS.Connect(ctx, m.AccountID.ValueString(), m.RoleARN.ValueString())
	if err != nil {
		failed(&resp.d, "connect the AWS account", err)
		return
	}
	warned(&resp.d, "registering the domains in SES", warnings)
}

func (r *awsRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan awsRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	r.connect(ctx, plan, d)
	resp.Diagnostics.Append(d.d...)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	}
}

func (r *awsRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state awsRoleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	all, err := r.client.Settings.Connections.AWS.List(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "read the AWS accounts", err)
		return
	}
	for _, a := range all {
		if a.ID == state.AccountID.ValueString() && a.RoleARN != "" {
			state.RoleARN = types.StringValue(a.RoleARN)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *awsRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan awsRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	r.connect(ctx, plan, d)
	resp.Diagnostics.Append(d.d...)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	}
}

func (r *awsRoleResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {}

// ── data "yet_settings_aws_launch" ───────────────────────────────────────────

type awsLaunchDataSource struct{ dataBase }

func newAWSLaunchDataSource() datasource.DataSource { return &awsLaunchDataSource{} }

type awsLaunchModel struct {
	TemplateURL types.String `tfsdk:"template_url"`
	Principal   types.String `tfsdk:"principal"`
}

func (d *awsLaunchDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_aws_launch"
}

func (d *awsLaunchDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		MarkdownDescription: "What every AWS account's stack is made with, Yet's side — Yet's CloudFormation template and the role Yet's server is — " +
			"for `aws_cloudformation_stack`, besides the account's `external_id`.",
		Attributes: map[string]dschema.Attribute{
			"template_url": computed("Yet's CloudFormation template."),
			"principal":    computed("Yet's server role: the stack's `YetPrincipal`, the only one its role trusts."),
		},
	}
}

func (d *awsLaunchDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	l, err := d.client.Settings.Connections.AWS.Launch(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "read what the AWS stacks are made with", err)
		return
	}
	if l.TemplateURL == "" {
		resp.Diagnostics.AddError("No AWS stack here", "Yet's server has no AWS identity of its own, so no account's role can trust it.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, awsLaunchModel{
		TemplateURL: types.StringValue(l.TemplateURL), Principal: types.StringValue(l.Principal),
	})...)
}
