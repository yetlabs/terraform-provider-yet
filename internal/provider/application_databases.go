package provider

import (
	"context"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

/*
The two ways an application keeps its data, as Cloud has them: a database
of its own — an add-on, run beside it on each of its stages — or one of its
environments' databases, attached to it on its stage there. Either way it
is handed the address in a variable: DATABASE_URL, REDIS_URL.
*/

var (
	envVarRule      = "capitals, digits and underscores"
	upperEnvVarName = stringvalidator.RegexMatches(regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`), "capitals, digits and underscores")
	replaceKeep     = []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()}
)

// ── yet_cloud_application_addon ─────────────────────────────────────────────────

type applicationAddonResource struct{ base }

func newApplicationAddonResource() resource.Resource { return &applicationAddonResource{} }

type applicationAddonModel struct {
	ID             types.String `tfsdk:"id"`
	ApplicationID  types.String `tfsdk:"application_id"`
	Kind           types.String `tfsdk:"kind"`
	EnvVar         types.String `tfsdk:"env_var"`
	Placement      types.String `tfsdk:"placement"`
	Version        types.String `tfsdk:"version"`
	MemoryMB       types.Int64  `tfsdk:"memory_mb"`
	Stopped        types.Bool   `tfsdk:"stopped"`
	Features       types.Set    `tfsdk:"features"`
	StudioRole     types.String `tfsdk:"studio_role"`
	StudioReadOnly types.Bool   `tfsdk:"studio_read_only"`
	Image          types.String `tfsdk:"image"`
	Buckets        types.List   `tfsdk:"buckets"`
	Chosen         types.List   `tfsdk:"bucket"`
}

// choiceModel is a stage's bucket as chosen for an s3 add-on.
type choiceModel struct {
	EnvironmentID types.String `tfsdk:"environment_id"`
	Name          types.String `tfsdk:"name"`
	Existing      types.Bool   `tfsdk:"existing"`
}

type bucketModel struct {
	EnvironmentID string `tfsdk:"environment_id"`
	Stage         string `tfsdk:"stage"`
	AWSAccountID  string `tfsdk:"aws_account_id"`
	Name          string `tfsdk:"name"`
	Region        string `tfsdk:"region"`
	Mode          string `tfsdk:"mode"`
	Identity      string `tfsdk:"identity"`
	Status        string `tfsdk:"status"`
	Problem       string `tfsdk:"problem"`
}

var bucketAttrTypes = map[string]attr.Type{
	"environment_id": types.StringType, "stage": types.StringType, "aws_account_id": types.StringType,
	"name": types.StringType, "region": types.StringType, "mode": types.StringType, "identity": types.StringType,
	"status": types.StringType, "problem": types.StringType,
}

func (r *applicationAddonResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_application_addon"
}

func (r *applicationAddonResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	str := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: desc, Optional: true, Computed: true, PlanModifiers: keep}
	}
	flag := func(desc string) schema.BoolAttribute {
		return schema.BoolAttribute{
			MarkdownDescription: desc, Optional: true, Computed: true,
			PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "An application's own database — Overlite, Postgres or Redis — its add-on: run beside it on " +
			"each of its stages, each stage's its own, and handed to it in a variable. Or an S3 bucket, `s3`: one on " +
			"each stage, in that stage's AWS account, reached as the stage's one identity there — by its EC2 servers' role, or with a key — " +
			"each bucket a policy on it.\n\n" +
			"-> **Note:** For one database that several applications share, make it an environment's — " +
			"`yet_cloud_database` — and attach it with `yet_cloud_database_attachment`.\n\n" +
			"~> **Note:** Destroyed, it stops at the agents' next look — its volume stays on each server. " +
			"Back it up first with the `yet_cloud_database_backup` action. An S3 bucket stays in its account: " +
			"only the application's way to it is taken away.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr,
			"application_id": schema.StringAttribute{
				MarkdownDescription: "The application it is of.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"kind": schema.StringAttribute{
				MarkdownDescription: "`overlite`, `postgres`, `redis`, `s3` or `bucketdb` — a Postgres in a bucket of each stage's, served beside each copy of the application, one to an application.",
				Required:            true,
				PlanModifiers:       replace,
				Validators:          []validator.String{stringvalidator.OneOf("overlite", "postgres", "redis", "s3", "bucketdb")},
			},
			"env_var": schema.StringAttribute{
				MarkdownDescription: "The variable the application is handed its address in — " + envVarRule +
					": `DATABASE_URL` for Overlite, Postgres and BucketDB, `REDIS_URL` for Redis, unless set. " +
					"For `s3`, the prefix of what it is handed — `S3_BUCKET`, `S3_REGION`, `S3_ENDPOINT` — `S3` unless set.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: replaceKeep,
				Validators:    []validator.String{upperEnvVarName},
			},
			"placement": schema.StringAttribute{
				MarkdownDescription: "On a pool: `shared` — one, on its first member — or `member`, one on each. " +
					"Overlite and Postgres are shared, Redis each member's, unless set.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: replaceKeep,
				Validators:    []validator.String{stringvalidator.OneOf("shared", "member")},
			},
			"version": str("The version it runs; the newest unless set."),
			"memory_mb": schema.Int64Attribute{
				MarkdownDescription: "The memory it may use, in MB: 128 to 65536, or `0` for no limit.", Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				Validators:    []validator.Int64{int64validator.Between(0, 65536)},
			},
			"stopped": flag("Whether it is stopped: kept, not running."),
			"features": schema.SetAttribute{
				MarkdownDescription: "What it runs with: `pgvector` for Postgres; `volatile`, `evict` for Redis.",
				ElementType:         types.StringType, Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"studio_role":      str("Who opens it in Cloud's studio: `viewer`, `developer` or `admin`."),
			"studio_read_only": flag("Whether the studio only reads."),
			"image":            schema.StringAttribute{Computed: true, MarkdownDescription: "The image it runs."},
			"bucket": schema.ListNestedAttribute{
				MarkdownDescription: "An `s3` add-on's buckets, chosen stage by stage: a new one's `name` — it begins " +
					"`yet-<company>-` — or, `existing`, one the stage's account has, reached and never made. A stage not " +
					"given one gets a bucket Yet names.",
				Optional:      true,
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"environment_id": schema.StringAttribute{Required: true, MarkdownDescription: "The stage's environment."},
					"name":           schema.StringAttribute{Required: true, MarkdownDescription: "The bucket's name."},
					"existing": schema.BoolAttribute{Optional: true, MarkdownDescription: "Whether it is one the account has: " +
						"not made, nor its settings changed."},
				}},
			},
			"buckets": schema.ListNestedAttribute{
				MarkdownDescription: "An `s3` add-on's buckets, one on each stage.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"environment_id": schema.StringAttribute{Computed: true, MarkdownDescription: "The stage's environment."},
					"stage":          schema.StringAttribute{Computed: true, MarkdownDescription: "The stage's name."},
					"aws_account_id": schema.StringAttribute{Computed: true, MarkdownDescription: "The AWS account it is in."},
					"name":           schema.StringAttribute{Computed: true, MarkdownDescription: "The bucket's name."},
					"region":         schema.StringAttribute{Computed: true, MarkdownDescription: "Its region."},
					"mode": schema.StringAttribute{Computed: true, MarkdownDescription: "How the application reaches it: " +
						"`native`, by its EC2 servers' role, or `key`, with the stage's access key."},
					"identity": schema.StringAttribute{Computed: true, MarkdownDescription: "Who reaches it in IAM: the stage's one user or role, " +
						"for all of the application's AWS add-ons there."},
					"status":  schema.StringAttribute{Computed: true, MarkdownDescription: "`pending`, `ready` or `failed`."},
					"problem": schema.StringAttribute{Computed: true, MarkdownDescription: "Why it failed, if it did."},
				}},
			},
		},
	}
}

func (m applicationAddonModel) input(ctx context.Context, d *diagsSink) yet.DatabaseInput {
	return yet.DatabaseInput{
		Version:        strPtr(m.Version),
		MemoryMB:       intPtr(m.MemoryMB),
		Stopped:        boolPtr(m.Stopped),
		Features:       stringsOf(ctx, m.Features, &d.d),
		StudioRole:     strPtr(m.StudioRole),
		StudioReadOnly: boolPtr(m.StudioReadOnly),
	}
}

func (m *applicationAddonModel) from(ctx context.Context, db *yet.Database, d *diagsSink) {
	m.ID = types.StringValue(db.ID)
	m.ApplicationID = types.StringValue(db.ApplicationID)
	m.Kind = types.StringValue(db.Kind)
	m.EnvVar = types.StringValue(db.EnvVar)
	m.Placement = types.StringValue(db.Placement)
	m.Version = types.StringValue(db.Version)
	m.MemoryMB = types.Int64Value(int64(db.MemoryMB))
	m.Stopped = types.BoolValue(db.Stopped)
	m.Features = setOf(ctx, db.Features, &d.d)
	m.StudioRole = types.StringValue(db.StudioRole)
	m.StudioReadOnly = types.BoolValue(db.StudioReadOnly)
	m.Image = types.StringValue(db.Image)
	buckets := make([]bucketModel, len(db.Buckets))
	for i, b := range db.Buckets {
		buckets[i] = bucketModel{
			EnvironmentID: b.EnvironmentID, Stage: b.Stage, AWSAccountID: b.AWSAccountID,
			Name: b.Name, Region: b.Region, Mode: b.Mode, Identity: b.Identity, Status: b.Status, Problem: b.Problem,
		}
	}
	var diags diag.Diagnostics
	m.Buckets, diags = types.ListValueFrom(ctx, types.ObjectType{AttrTypes: bucketAttrTypes}, buckets)
	d.d.Append(diags...)
}

func (r *applicationAddonResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan applicationAddonModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var chosen []choiceModel
	if !plan.Chosen.IsNull() && !plan.Chosen.IsUnknown() {
		resp.Diagnostics.Append(plan.Chosen.ElementsAs(ctx, &chosen, false)...)
	}
	var choices []yet.BucketChoice
	for _, c := range chosen {
		choices = append(choices, yet.BucketChoice{EnvironmentID: c.EnvironmentID.ValueString(), Name: c.Name.ValueString(), Existing: c.Existing.ValueBool()})
	}
	db, err := r.client.Cloud.Applications.CreateAddon(ctx, plan.ApplicationID.ValueString(), yet.AddonInput{
		Kind: plan.Kind.ValueString(), EnvVar: plan.EnvVar.ValueString(), Placement: plan.Placement.ValueString(), Buckets: choices,
	})
	if err != nil {
		failed(&resp.Diagnostics, "add the application's database", err)
		return
	}
	// How it runs, set right after: Cloud makes one with its kind's defaults.
	d := &diagsSink{}
	if in := plan.input(ctx, d); in != (yet.DatabaseInput{}) {
		if db, err = r.client.Cloud.Databases.Update(ctx, db.ID, in); err != nil {
			failed(&resp.Diagnostics, "set the application's database up", err)
			return
		}
	}
	plan.from(ctx, db, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *applicationAddonResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state applicationAddonModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	db, err := r.client.Cloud.Databases.Get(ctx, state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the application's database", err)
		}
		return
	}
	// An environment's database by that id is not one of these.
	if db.ApplicationID == "" {
		resp.State.RemoveResource(ctx)
		return
	}
	d := &diagsSink{}
	state.from(ctx, db, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *applicationAddonResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan applicationAddonModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	db, err := r.client.Cloud.Databases.Update(ctx, plan.ID.ValueString(), plan.input(ctx, d))
	if err != nil {
		failed(&resp.Diagnostics, "change the application's database", err)
		return
	}
	plan.from(ctx, db, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *applicationAddonResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state applicationAddonModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	err := r.client.Cloud.Applications.DeleteAddon(ctx, state.ApplicationID.ValueString(), state.ID.ValueString())
	if err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "remove the application's database", err)
	}
}

// ImportState takes the database's id: Cloud says whose it is.
func (r *applicationAddonResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importID(ctx, req, resp)
}

// ── yet_cloud_database_attachment ────────────────────────────────────────────

type databaseAttachmentResource struct{ base }

func newDatabaseAttachmentResource() resource.Resource { return &databaseAttachmentResource{} }

type databaseAttachmentModel struct {
	ID            types.String `tfsdk:"id"`
	ApplicationID types.String `tfsdk:"application_id"`
	DatabaseID    types.String `tfsdk:"database_id"`
	EnvVar        types.String `tfsdk:"env_var"`
}

func (r *databaseAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_database_attachment"
}

func (r *databaseAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An environment's database — `yet_cloud_database` — handed to an application on its stage " +
			"there, in a variable. Its other stages are not handed it.\n\n" +
			"~> **Note:** The application must have a stage on the database's environment, and no other variable " +
			"of that name — its own `env`, a database of its own, another attachment.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "`<application_id>/<database_id>`.",
				Computed:            true,
				PlanModifiers:       keep,
			},
			"application_id": schema.StringAttribute{
				MarkdownDescription: "The application it is handed to.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"database_id": schema.StringAttribute{
				MarkdownDescription: "The environment's database.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"env_var": schema.StringAttribute{
				MarkdownDescription: "The variable it is handed in — " + envVarRule + ": the database's own, `DATABASE_URL` or `REDIS_URL`, unless set.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       replaceKeep,
				Validators:          []validator.String{upperEnvVarName},
			},
		},
	}
}

func (r *databaseAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan databaseAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	app, db := plan.ApplicationID.ValueString(), plan.DatabaseID.ValueString()
	got, err := r.client.Cloud.Applications.Attach(ctx, app, db, plan.EnvVar.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "attach the database", err)
		return
	}
	plan.ID = types.StringValue(app + "/" + db)
	plan.EnvVar = types.StringValue(got.AttachedAs)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *databaseAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state databaseAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	app, db := state.ApplicationID.ValueString(), state.DatabaseID.ValueString()
	all, err := r.client.Cloud.Applications.Addons(ctx, app)
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the application's databases", err)
		}
		return
	}
	for _, x := range all {
		if x.ID == db && x.AttachedAs != "" {
			state.ID = types.StringValue(app + "/" + db)
			state.EnvVar = types.StringValue(x.AttachedAs)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

// Update has nothing to do: every attribute makes a new attachment.
func (r *databaseAttachmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan databaseAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *databaseAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state databaseAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	err := r.client.Cloud.Applications.Detach(ctx, state.ApplicationID.ValueString(), state.DatabaseID.ValueString())
	if err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "detach the database", err)
	}
}

// ImportState takes "<application_id>/<database_id>".
func (r *databaseAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	app, db, ok := strings.Cut(req.ID, "/")
	if !ok || app == "" || db == "" {
		resp.Diagnostics.AddError("Unexpected import id", "Import an attachment as <application_id>/<database_id>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("application_id"), app)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database_id"), db)...)
}
