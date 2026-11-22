package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

// ── yet_cloud_environment ────────────────────────────────────────────────────

type environmentResource struct{ base }

func newEnvironmentResource() resource.Resource { return &environmentResource{} }

type environmentModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	Color      types.String `tfsdk:"color"`
	RuntimeID  types.String `tfsdk:"runtime_id"`
	PoolID     types.String `tfsdk:"pool_id"`
	ZoneID     types.String `tfsdk:"zone_id"`
	Prefix     types.String `tfsdk:"prefix"`
	Domain     types.String `tfsdk:"domain"`
	AWSAccount types.String `tfsdk:"aws_account_id"`
	Lambda     types.List   `tfsdk:"functions"`
}

func (r *environmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_environment"
}

func environmentAttributes() map[string]schema.Attribute {
	where := []validator.String{stringvalidator.ExactlyOneOf(path.MatchRoot("runtime_id"), path.MatchRoot("pool_id"))}
	return map[string]schema.Attribute{
		"id": idAttr,
		"name": schema.StringAttribute{
			MarkdownDescription: "Its name: " + kubeNameRule + ". Only shown — its stages, access and what runs go by its id — so it changes in place.",
			Required:            true,
			Validators:          cloudName(),
		},
		"color": schema.StringAttribute{
			Optional: true, Computed: true, Default: stringdefault.StaticString("gray"),
			MarkdownDescription: "How Cloud draws it: `" + joinTicks(colors) + "`.",
			Validators:          []validator.String{stringvalidator.OneOf(colors...)},
		},
		"runtime_id": schema.StringAttribute{
			MarkdownDescription: "The runtime it runs on — or `pool_id`. It moves only while nothing is deployed there, or between a pool and one of its members.",
			Optional:            true,
			Validators:          where,
		},
		"pool_id": schema.StringAttribute{
			MarkdownDescription: "The pool it runs on — or `runtime_id`.",
			Optional:            true,
			Validators:          where,
		},
		"zone_id": schema.StringAttribute{
			MarkdownDescription: "The DNS zone it answers in. Without one, its applications get temporary addresses.",
			Optional:            true,
		},
		"prefix": schema.StringAttribute{
			MarkdownDescription: "What goes before the zone's name: `staging` makes it `staging.<zone>`. Its root without one.",
			Optional:            true,
		},
		"domain": schema.StringAttribute{
			MarkdownDescription: "Where its applications answer: `<subdomain>.<domain>`.",
			Computed:            true,
		},
		"aws_account_id": schema.StringAttribute{
			MarkdownDescription: "The AWS account its applications' S3 buckets are made in — one connected for `applications`. " +
				"Without one, its buckets wait. A stage on Yet's EC2 servers has them in its servers' account.",
			Optional: true,
		},
		"functions": schema.ListAttribute{
			MarkdownDescription: "Lambda runtimes (`yet_cloud_runtime` of stack `aws:lambda`, on a serverless `yet_cloud_server`) beside its runtimes, " +
				"its applications may run on as functions — each stage choosing, by `yet_cloud_application`'s `stage_run` and `stage_function`. " +
				"Empty, Lambda is off; one goes only once no stage runs as a function on it. For Lambda alone, its `runtime_id` is one instead.",
			ElementType: types.StringType,
			Optional:    true,
		},
	}
}

func (r *environmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Where a stage of the company's applications runs — staging, production — on a runtime or a pool, and the domain it answers at.\n\n" +
			"~> **Note:** It moves to another runtime or pool only while nothing is deployed there — or between a pool and one of its members.",
		Attributes: environmentAttributes(),
	}
}

func (m *environmentModel) from(e *yet.Environment) {
	m.ID = types.StringValue(e.ID)
	m.Name = types.StringValue(e.Name)
	m.Color = types.StringValue(e.Color)
	m.RuntimeID = orNull(e.RuntimeID)
	m.PoolID = orNull(e.PoolID)
	m.ZoneID = orNull(e.ZoneID)
	m.Prefix = orNull(e.Prefix)
	m.Domain = types.StringValue(e.Domain)
	m.AWSAccount = orNull(e.AWSAccountID)
	if len(e.Functions) > 0 || !m.Lambda.IsNull() {
		m.Lambda, _ = types.ListValueFrom(context.Background(), types.StringType, nonNil(e.Functions))
	}
}

// input is everything about where it runs, said whole: an empty value takes it away.
func (m environmentModel) input() yet.EnvironmentInput {
	text := func(v types.String) *string { s := v.ValueString(); return &s }
	return yet.EnvironmentInput{
		Name:         m.Name.ValueString(),
		Color:        strPtr(m.Color),
		RuntimeID:    text(m.RuntimeID),
		PoolID:       text(m.PoolID),
		ZoneID:       text(m.ZoneID),
		Prefix:       text(m.Prefix),
		AWSAccountID: text(m.AWSAccount),
		Functions:    regionsOf(m.Lambda),
	}
}

func (r *environmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan environmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	e, err := r.client.Cloud.Environments.Create(ctx, plan.input())
	if err != nil {
		failed(&resp.Diagnostics, "create the environment", err)
		return
	}
	plan.from(e)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *environmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state environmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	e, err := r.client.Cloud.Environments.Get(ctx, state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the environment", err)
		}
		return
	}
	state.from(e)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *environmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan environmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	e, err := r.client.Cloud.Environments.Update(ctx, plan.ID.ValueString(), plan.input())
	if err != nil {
		failed(&resp.Diagnostics, "change the environment", err)
		return
	}
	plan.from(e)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *environmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state environmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if err := r.client.Cloud.Environments.Delete(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the environment", err)
	}
}

func (r *environmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importID(ctx, req, resp)
}

// ── yet_cloud_namespace ──────────────────────────────────────────────────────

type namespaceResource struct{ base }

func newNamespaceResource() resource.Resource { return &namespaceResource{} }

type namespaceModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

func (r *namespaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_namespace"
}

func (r *namespaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A group of applications: a product, a team.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr,
			"name": schema.StringAttribute{
				MarkdownDescription: "Its name.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 60)},
			},
		},
	}
}

func (r *namespaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan namespaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ns, err := r.client.Cloud.Namespaces.Create(ctx, plan.Name.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "create the namespace", err)
		return
	}
	plan.ID, plan.Name = types.StringValue(ns.ID), types.StringValue(ns.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *namespaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state namespaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ns, err := r.client.Cloud.Namespaces.Get(ctx, state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the namespace", err)
		}
		return
	}
	state.ID, state.Name = types.StringValue(ns.ID), types.StringValue(ns.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *namespaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan namespaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ns, err := r.client.Cloud.Namespaces.Rename(ctx, plan.ID.ValueString(), plan.Name.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "rename the namespace", err)
		return
	}
	plan.Name = types.StringValue(ns.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *namespaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state namespaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if err := r.client.Cloud.Namespaces.Delete(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the namespace", err)
	}
}

func (r *namespaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importID(ctx, req, resp)
}

// ── yet_cloud_application ────────────────────────────────────────────────────

type applicationResource struct{ base }

func newApplicationResource() resource.Resource { return &applicationResource{} }

var (
	_ resource.ResourceWithValidateConfig = (*applicationResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*applicationResource)(nil)
)

type applicationModel struct {
	ID              types.String `tfsdk:"id"`
	AddonsOnDelete  types.String `tfsdk:"addons_on_delete"`
	NamespaceID     types.String `tfsdk:"namespace_id"`
	Name            types.String `tfsdk:"name"`
	Stages          types.List   `tfsdk:"stages"`
	Subdomain       types.String `tfsdk:"subdomain"`
	Port            types.Int64  `tfsdk:"port"`
	Ports           types.List   `tfsdk:"ports"`
	Readiness       types.Object `tfsdk:"readiness"`
	Liveness        types.Object `tfsdk:"liveness"`
	Replicas        types.Int64  `tfsdk:"replicas"`
	MaxReplicas     types.Int64  `tfsdk:"max_replicas"`
	CPUMillicores   types.Int64  `tfsdk:"cpu_millicores"`
	MemoryMB        types.Int64  `tfsdk:"memory_mb"`
	ScaleCPU        types.Int64  `tfsdk:"scale_cpu"`
	ScaleMemory     types.Int64  `tfsdk:"scale_memory"`
	TimeoutSeconds  types.Int64  `tfsdk:"timeout_seconds"`
	FunctionRoles   types.Map    `tfsdk:"function_roles"`
	FunctionPolicy  types.String `tfsdk:"function_policy"`
	SecretEnv       types.Map    `tfsdk:"secret_env"`
	Env             types.Map    `tfsdk:"env"`
	StageEnv        types.Map    `tfsdk:"stage_env"`
	StageRun        types.Map    `tfsdk:"stage_run"`
	StageFunction   types.Map    `tfsdk:"stage_function"`
	EnvironmentSize types.Map    `tfsdk:"environment_size"`
	Repository      types.String `tfsdk:"repository"`
	Branch          types.String `tfsdk:"branch"`
	BuildRoot       types.String `tfsdk:"build_root"`
	Dockerfile      types.String `tfsdk:"dockerfile"`
	InternalURL     types.String `tfsdk:"internal_url"`
	BuildOn         types.String `tfsdk:"build_on"`
	TagPattern      types.String `tfsdk:"tag_pattern"`
	AutoPromote     types.List   `tfsdk:"auto_promote"`
	YaPEveryone     types.Bool   `tfsdk:"yap_everyone"`
	YaPAllowed      types.List   `tfsdk:"yap_allowed"`
}

// environmentSizeModel is one environment's own size; a field null takes the application's.
type environmentSizeModel struct {
	Replicas      types.Int64 `tfsdk:"replicas"`
	MaxReplicas   types.Int64 `tfsdk:"max_replicas"`
	CPUMillicores types.Int64 `tfsdk:"cpu_millicores"`
	MemoryMB      types.Int64 `tfsdk:"memory_mb"`
	ScaleCPU      types.Int64 `tfsdk:"scale_cpu"`
	ScaleMemory   types.Int64 `tfsdk:"scale_memory"`
}

var environmentSizeAttrTypes = map[string]attr.Type{
	"replicas": types.Int64Type, "max_replicas": types.Int64Type, "cpu_millicores": types.Int64Type,
	"memory_mb": types.Int64Type, "scale_cpu": types.Int64Type, "scale_memory": types.Int64Type,
}

// int64Of is a field yet may leave out: null when it does.
func int64Of(n *int) types.Int64 {
	if n == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*n))
}

func (r *applicationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_application"
}

func (r *applicationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	str := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: desc, Optional: true, Computed: true, PlanModifiers: keep}
	}
	// An environment's own field is said or not; not said, the application's.
	envNum := func(desc string) schema.Int64Attribute {
		return schema.Int64Attribute{MarkdownDescription: desc, Optional: true}
	}
	num := func(desc string) schema.Int64Attribute {
		return schema.Int64Attribute{
			MarkdownDescription: desc, Optional: true, Computed: true,
			PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
		}
	}
	// A threshold is a step of 5 between 10 and 95; memory's may be 0 too.
	scale := func(a schema.Int64Attribute, least int64) schema.Int64Attribute {
		steps := []int64{}
		if least == 0 {
			steps = append(steps, 0)
		}
		for n := int64(10); n <= 95; n += 5 {
			steps = append(steps, n)
		}
		a.Validators = []validator.Int64{int64validator.OneOf(steps...)}
		return a
	}
	// With ports, port is the default's: one or the other is said.
	port := num("The default port's number: `8080` unless set. With `ports`, theirs.")
	port.Validators = []validator.Int64{int64validator.ConflictsWith(path.MatchRoot("ports"))}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Something the company runs, through its stages — one environment each, in order: staging, then production. " +
			"Built from a GitHub repository, or released from an image.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr,
			"namespace_id": schema.StringAttribute{
				MarkdownDescription: "The namespace it belongs to.",
				Required:            true,
			},
			"addons_on_delete": schema.StringAttribute{
				MarkdownDescription: "What becomes of its add-ons when it is destroyed: `keep` (the default) — they run on where they were, " +
					"for the applications attached to them, until one adopts them (`yet_cloud_database_adopt`) — or `delete`, with it.",
				Optional:   true,
				Computed:   true,
				Default:    stringdefault.StaticString("keep"),
				Validators: []validator.String{stringvalidator.OneOf("keep", "delete")},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Its name.",
				Required:            true,
			},
			"stages": schema.ListAttribute{
				MarkdownDescription: "Its environments' ids, in the order releases go through them. Each on a runtime of its own.",
				ElementType:         types.StringType,
				Required:            true,
			},
			"subdomain":    str("The name it answers at under its stages' domains; its name's when left out, `@` for the domain's root."),
			"port":         port,
			"ports":        portsAttr(),
			"readiness":    probeAttr("Whether a copy may take requests: a check on one of its ports."),
			"liveness":     probeAttr("Whether a copy is alive, restarted when not: a check on one of its ports."),
			"replicas":     num("How many run in each stage: `1` unless set."),
			"max_replicas": num("Up to how many it scales to, when more than `replicas`."),
			"cpu_millicores": num("What each copy may use at most, in thousandths of a core — `50` to `16000` — and what it asks for; " +
				"the autoscaler measures against it. `0`, no limit."),
			"memory_mb": num("Each copy's memory at most, and what it asks for, in MB — `32` to `65536`. `0`, no limit."),
			"scale_cpu": scale(num("Where it scales out on processors, when it grows, as a percent of what its copies ask for — "+
				"`10` to `95`, in steps of `5`: `70` unless set."), 10),
			"scale_memory": scale(num("Where it scales out on memory, when it grows, as a percent of `memory_mb` — "+
				"`10` to `95`, in steps of `5`. `0`, never on memory: the default."), 0),
			"timeout_seconds": schema.Int64Attribute{
				MarkdownDescription: "How long a stage that runs as a function may take on one call, in seconds — `1` to `900`; `0`, 60. " +
					"Through its address CloudFront waits 60 at most; a schedule (`yet_cloud_application_schedule`) calls it straight.",
				Optional:      true,
				Computed:      true,
				Validators:    []validator.Int64{int64validator.Between(0, 900)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"secret_env": schema.MapAttribute{
				MarkdownDescription: "Its variables from Vault, in every stage: a name, a secret (`yet_vault_secret`'s `id`) of a vault that lets it " +
					"(`yet_vault_grant`). On a runtime in a Kubernetes Secret beside it, on Lambda in its function's variables — never in Yet — " +
					"and in its account's Parameter Store under `/yet/<company>/<application id>/<name>`, for it to read itself.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"function_policy": schema.StringAttribute{
				MarkdownDescription: "What else its functions' role may do — the bucket of its database, a queue: an IAM policy document " +
					"(`jsonencode({ Version = \"2012-10-17\", Statement = [...] })`), 6 KB at most, put on the role as it is made, " +
					"so its first deploy already has it. Its own buckets it reaches without.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: keep,
			},
			"function_roles": schema.MapAttribute{
				MarkdownDescription: "The role each stage that runs as a function runs as, its ARN by environment id — said once the role is " +
					"there, as the function is first put in. What else it may reach goes in `function_policy`, which it is made with.",
				ElementType:   types.StringType,
				Computed:      true,
				PlanModifiers: []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
			},
			"env": schema.MapAttribute{
				MarkdownDescription: "Its variables, in every stage.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
			},
			"stage_env": schema.MapAttribute{
				MarkdownDescription: "Each stage's own variables, by environment id, over `env`.",
				ElementType:         types.MapType{ElemType: types.StringType},
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
			},
			"stage_run": schema.MapAttribute{
				MarkdownDescription: "Where each stage runs, by environment id: `lambda`, as a function in an environment on a Lambda runtime (`aws:lambda`) or with `functions` — " +
					"on a `port` from 1024, at its subdomain or the root — no YaP ports — one Yet builds is built in its account, on CodeBuild; " +
					"a stage left out runs on its runtimes.",
				ElementType:   types.StringType,
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
			},
			"stage_function": schema.MapAttribute{
				MarkdownDescription: "Which of its environment's Lambda runtimes each stage that runs as a function runs on, by environment id — " +
					"the first when left out. Another region of the same account moves it with no downtime: made there first, its distribution — address, certificate, records — then pointed there, and only then the old one taken away. Another account makes it anew.",
				ElementType:   types.StringType,
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
			},
			"environment_size": schema.MapNestedAttribute{
				MarkdownDescription: "Each environment's own size, by environment id: each field said takes the place of the application's " +
					"of the same name there, by the same rules; one left out takes the application's.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"replicas":       envNum("How many run there."),
					"max_replicas":   envNum("Up to how many it scales to there, when more than its `replicas`."),
					"cpu_millicores": envNum("What each copy may use at most there, in thousandths of a core — `50` to `16000`. `0`, no limit."),
					"memory_mb":      envNum("Each copy's memory at most there, in MB — `32` to `65536`. `0`, no limit."),
					"scale_cpu":      scale(envNum("Where it scales out on processors there, `10` to `95`, in steps of `5`."), 10),
					"scale_memory":   scale(envNum("Where it scales out on memory there, `10` to `95`, in steps of `5`. `0`, never on memory."), 0),
				}},
			},
			"repository": str("The GitHub repository it is built from, `owner/name`, through Cloud's GitHub App."),
			"branch":     str("The branch whose pushes are built."),
			"build_root": str("Of a monorepo, the folder it is built from — the build's context, and where Railpack looks; " +
				"the whole repository unless set."),
			"dockerfile": str("Of a monorepo, its Dockerfile, from the repository's root — `leads/Dockerfile` — so the build " +
				"may reach what is beside its folder. Unless set, the folder's `Dockerfile`, or Railpack's plan."),
			"internal_url": schema.StringAttribute{
				Computed: true, PlanModifiers: keep,
				MarkdownDescription: "Where its default port answers inside its stage's cluster — `http://<id>.<namespace id>.svc.cluster.local`, " +
					"on port 80; another port of its, on its own number — to the applications of its namespace alone.",
			},
			"build_on": schema.StringAttribute{
				MarkdownDescription: "What makes a build, once it has a `repository`: `\"\"` (the default), every push to its `branch` — " +
					"`main` unless set — or `tag`, every tag matching `tag_pattern`. Each build is released to the first stage " +
					"as it is done; a release by `yet_cloud_application_release` needs none of this.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: keep,
				Validators:    []validator.String{stringvalidator.OneOf("", "tag")},
			},
			"tag_pattern": str("The tags that make a build, when it builds on tags: `v*`."),
			"auto_promote": schema.ListAttribute{
				MarkdownDescription: "The stages that promote to the next by themselves, once running.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"yap_everyone": schema.BoolAttribute{
				MarkdownDescription: "Whether everyone at the company — today and whoever joins — gets in at its YaP ports; if not, " +
					"only `yap_allowed`. Its first YaP port lets everyone in, unless said.",
				Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"yap_allowed": schema.ListAttribute{
				MarkdownDescription: "The people and groups (the company's lists) who get in at its YaP ports, by address, when not everyone.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (m applicationModel) input(ctx context.Context, d *diagsSink) yet.ApplicationInput {
	in := yet.ApplicationInput{
		NamespaceID:    strPtr(m.NamespaceID),
		Name:           strPtr(m.Name),
		Stages:         stringsOf(ctx, m.Stages, &d.d),
		Subdomain:      strPtr(m.Subdomain),
		Port:           intPtr(m.Port),
		Replicas:       intPtr(m.Replicas),
		MaxReplicas:    intPtr(m.MaxReplicas),
		CPUMillicores:  intPtr(m.CPUMillicores),
		MemoryMB:       intPtr(m.MemoryMB),
		ScaleCPU:       intPtr(m.ScaleCPU),
		ScaleMemory:    intPtr(m.ScaleMemory),
		TimeoutSeconds: intPtr(m.TimeoutSeconds),
		FunctionPolicy: strPtr(m.FunctionPolicy),
		SecretEnv:      secretEnvOf(ctx, m.SecretEnv, &d.d),
		Repository:     strPtr(m.Repository),
		BuildRoot:      strPtr(m.BuildRoot),
		Dockerfile:     strPtr(m.Dockerfile),
		Branch:         strPtr(m.Branch),
		BuildOn:        strPtr(m.BuildOn),
		TagPattern:     strPtr(m.TagPattern),
		AutoPromote:    stringsOf(ctx, m.AutoPromote, &d.d),
		Readiness:      probeIn(ctx, m.Readiness, d),
		NoReadiness:    m.Readiness.IsNull(),
		Liveness:       probeIn(ctx, m.Liveness, d),
		NoLiveness:     m.Liveness.IsNull(),
		YaPAllowed:     stringsOf(ctx, m.YaPAllowed, &d.d),
	}
	if known(m.YaPEveryone) {
		in.YaPEveryone = m.YaPEveryone.ValueBoolPointer()
	}
	if ports := portsIn(ctx, m.Ports, d); ports != nil && !m.Ports.IsUnknown() {
		list := make([]yet.AppPort, len(ports))
		for i, p := range ports {
			list[i] = yet.AppPort{
				Name: p.Name.ValueString(), Port: int(p.Port.ValueInt64()), Protocol: p.Protocol.ValueString(),
				Public: p.Public.ValueBool(), YaP: p.YaP.ValueBool(), Default: p.Default.ValueBool(), Address: p.Address.ValueString(),
			}
		}
		in.Ports = &list
	}
	if known(m.Env) {
		env := map[string]string{}
		d.d.Append(m.Env.ElementsAs(ctx, &env, false)...)
		in.Env = &env
	}
	if known(m.StageEnv) {
		stageEnv := map[string]map[string]string{}
		d.d.Append(m.StageEnv.ElementsAs(ctx, &stageEnv, false)...)
		in.StageEnv = &stageEnv
	}
	if known(m.StageRun) {
		stageRun := map[string]string{}
		d.d.Append(m.StageRun.ElementsAs(ctx, &stageRun, false)...)
		in.StageRun = &stageRun
	}
	if known(m.StageFunction) {
		stageFunction := map[string]string{}
		d.d.Append(m.StageFunction.ElementsAs(ctx, &stageFunction, false)...)
		in.StageFunction = &stageFunction
	}
	if known(m.EnvironmentSize) {
		models := map[string]environmentSizeModel{}
		d.d.Append(m.EnvironmentSize.ElementsAs(ctx, &models, false)...)
		environmentSize := map[string]yet.EnvironmentSize{}
		for env, st := range models {
			environmentSize[env] = yet.EnvironmentSize{
				Replicas: intPtr(st.Replicas), MaxReplicas: intPtr(st.MaxReplicas),
				CPUMillicores: intPtr(st.CPUMillicores), MemoryMB: intPtr(st.MemoryMB),
				ScaleCPU: intPtr(st.ScaleCPU), ScaleMemory: intPtr(st.ScaleMemory),
			}
		}
		in.EnvironmentSize = &environmentSize
	}
	return in
}

func (m *applicationModel) from(ctx context.Context, a *yet.Application, d *diagsSink) {
	m.ID = types.StringValue(a.ID)
	// Terraform's own, not yet's: imported, it is the default.
	if !known(m.AddonsOnDelete) {
		m.AddonsOnDelete = types.StringValue("keep")
	}
	m.NamespaceID = types.StringValue(a.NamespaceID)
	m.Name = types.StringValue(a.Name)
	m.Stages = listOf(ctx, a.Stages, &d.d)
	// The root is kept as "@" and shown as no subdomain: what was said stays.
	if !(m.Subdomain.ValueString() == "@" && a.Subdomain == "") {
		m.Subdomain = types.StringValue(a.Subdomain)
	}
	m.Port = types.Int64Value(int64(a.Port))
	m.Ports = portsOf(ctx, a.Ports, d)
	m.Readiness = probeOf(ctx, a.Readiness, d)
	m.Liveness = probeOf(ctx, a.Liveness, d)
	m.Replicas = types.Int64Value(int64(a.Replicas))
	m.MaxReplicas = types.Int64Value(int64(a.MaxReplicas))
	m.CPUMillicores = types.Int64Value(int64(a.CPUMillicores))
	m.MemoryMB = types.Int64Value(int64(a.MemoryMB))
	m.ScaleCPU = types.Int64Value(int64(a.ScaleCPU))
	m.ScaleMemory = types.Int64Value(int64(a.ScaleMemory))
	m.TimeoutSeconds = types.Int64Value(int64(a.TimeoutSeconds))
	m.FunctionPolicy = types.StringValue(a.FunctionPolicy)
	if len(a.SecretEnv) > 0 || !m.SecretEnv.IsNull() {
		m.SecretEnv = mapOf(ctx, a.SecretEnv, &d.d)
	}
	roles := map[string]string{}
	for _, f := range a.Functions {
		if f.RoleARN != "" {
			roles[f.EnvironmentID] = f.RoleARN
		}
	}
	m.FunctionRoles = mapOf(ctx, roles, &d.d)
	m.Env = mapOf(ctx, a.Env, &d.d)
	stageEnv := a.StageEnv
	if stageEnv == nil {
		stageEnv = map[string]map[string]string{}
	}
	v, diags := types.MapValueFrom(ctx, types.MapType{ElemType: types.StringType}, stageEnv)
	d.d.Append(diags...)
	m.StageEnv = v
	m.StageRun = mapOf(ctx, a.StageRun, &d.d)
	m.StageFunction = mapOf(ctx, a.StageFunction, &d.d)
	environmentSize := map[string]environmentSizeModel{}
	for env, st := range a.EnvironmentSize {
		environmentSize[env] = environmentSizeModel{
			Replicas: int64Of(st.Replicas), MaxReplicas: int64Of(st.MaxReplicas),
			CPUMillicores: int64Of(st.CPUMillicores), MemoryMB: int64Of(st.MemoryMB),
			ScaleCPU: int64Of(st.ScaleCPU), ScaleMemory: int64Of(st.ScaleMemory),
		}
	}
	v, diags = types.MapValueFrom(ctx, types.ObjectType{AttrTypes: environmentSizeAttrTypes}, environmentSize)
	d.d.Append(diags...)
	m.EnvironmentSize = v
	m.Repository = types.StringValue(a.Repository)
	m.BuildRoot = types.StringValue(a.BuildRoot)
	m.Dockerfile = types.StringValue(a.Dockerfile)
	m.InternalURL = types.StringValue(a.InternalURL)
	m.Branch = types.StringValue(a.Branch)
	m.BuildOn = types.StringValue(a.BuildOn)
	m.TagPattern = types.StringValue(a.TagPattern)
	m.AutoPromote = listOf(ctx, a.AutoPromote, &d.d)
	m.YaPEveryone = types.BoolValue(a.YaPEveryone)
	m.YaPAllowed = listOf(ctx, a.YaPAllowed, &d.d)
}

func (r *applicationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan applicationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	in := plan.input(ctx, d)
	if resp.Diagnostics.Append(d.d...); resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.Cloud.Applications.Create(ctx, in)
	if err != nil {
		failed(&resp.Diagnostics, "create the application", err)
		return
	}
	plan.from(ctx, a, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *applicationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state applicationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.Cloud.Applications.Get(ctx, state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the application", err)
		}
		return
	}
	d := &diagsSink{}
	state.from(ctx, a, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *applicationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan applicationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	in := plan.input(ctx, d)
	if resp.Diagnostics.Append(d.d...); resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.Cloud.Applications.Update(ctx, plan.ID.ValueString(), in)
	if err != nil {
		failed(&resp.Diagnostics, "change the application", err)
		return
	}
	plan.from(ctx, a, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *applicationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state applicationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	del := r.client.Cloud.Applications.Delete
	if state.AddonsOnDelete.ValueString() == "delete" {
		del = r.client.Cloud.Applications.DeleteWithAddons
	}
	if err := del(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the application", err)
	}
}

func (r *applicationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importID(ctx, req, resp)
}

// ── yet_cloud_database ───────────────────────────────────────────────────────

type databaseResource struct{ base }

func newDatabaseResource() resource.Resource { return &databaseResource{} }

type databaseModel struct {
	ID             types.String `tfsdk:"id"`
	Kind           types.String `tfsdk:"kind"`
	Name           types.String `tfsdk:"name"`
	EnvironmentID  types.String `tfsdk:"environment_id"`
	Version        types.String `tfsdk:"version"`
	MemoryMB       types.Int64  `tfsdk:"memory_mb"`
	Stopped        types.Bool   `tfsdk:"stopped"`
	Features       types.Set    `tfsdk:"features"`
	Hostname       types.String `tfsdk:"hostname"`
	StudioRole     types.String `tfsdk:"studio_role"`
	StudioReadOnly types.Bool   `tfsdk:"studio_read_only"`
	EnvVar         types.String `tfsdk:"env_var"`
	Image          types.String `tfsdk:"image"`
	BackupEvery    types.Int64  `tfsdk:"backup_every"`
	BackupKeep     types.Int64  `tfsdk:"backup_keep"`
	BackupTarget   types.String `tfsdk:"backup_target"`
	BucketName     types.String `tfsdk:"bucket_name"`
	BucketExisting types.Bool   `tfsdk:"bucket_existing"`
}

func (r *databaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_database"
}

func (r *databaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
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
		MarkdownDescription: "An environment's database — Overlite, Postgres or Redis — run there for the applications attached to it.\n\n" +
			"~> **Note:** Destroyed, it stops — its volume stays on the server. Back it up first with the " +
			"`yet_cloud_database_backup` action.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr,
			"kind": schema.StringAttribute{
				MarkdownDescription: "`overlite`, `postgres`, `redis` or `bucketdb` — in a bucket of the environment's AWS account, served beside each application attached to it.",
				Required:            true,
				PlanModifiers:       replace,
				Validators:          []validator.String{stringvalidator.OneOf("overlite", "postgres", "redis", "bucketdb")},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Its name in the environment: " + kubeNameRule + ".",
				Required:            true,
				Validators:          cloudName(),
			},
			"environment_id": schema.StringAttribute{
				MarkdownDescription: "The environment it runs in.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"version": str("The version it runs; the newest unless set."),
			"memory_mb": schema.Int64Attribute{
				MarkdownDescription: "The memory it may use, in MB.", Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"stopped": flag("Whether it is stopped: kept, not running."),
			"features": schema.SetAttribute{
				MarkdownDescription: "What it runs with: `pgvector` for Postgres; `volatile`, `evict` for Redis.",
				ElementType:         types.StringType, Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"hostname":         str("Its name on the edge, when it answers outside: in one of the company's DNS zones, once pointed at Yet."),
			"studio_role":      str("Who opens it in Cloud's studio: `viewer`, `developer` or `admin`."),
			"studio_read_only": flag("Whether the studio only reads."),
			"env_var":          schema.StringAttribute{Computed: true, PlanModifiers: keep, MarkdownDescription: "The variable attached applications find it in."},
			"bucket_name": schema.StringAttribute{
				MarkdownDescription: "A `bucketdb`'s bucket, as chosen: a new one's name — it begins `yet-<company>-` — " +
					"or, `bucket_existing`, one the environment's account has. Unset, Yet names it.",
				Optional: true, PlanModifiers: replace,
			},
			"bucket_existing": schema.BoolAttribute{
				MarkdownDescription: "Whether `bucket_name` is one the account has: reached, never made.",
				Optional:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"image": schema.StringAttribute{Computed: true, MarkdownDescription: "The image it runs."},
			"backup_every": schema.Int64Attribute{
				MarkdownDescription: "How often it is copied, in hours: 1 to 168, or `0` for never. " +
					"Copies are kept for an Overlite on a runtime of the company's.",
				Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				Validators:    []validator.Int64{int64validator.Between(0, 168)},
			},
			"backup_keep": schema.Int64Attribute{
				MarkdownDescription: "How many of its copies are kept, the newest: 1 to 365. Cloud keeps 7 while it is `0`, never set.",
				Optional:            true, Computed: true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				Validators:    []validator.Int64{int64validator.Between(1, 365)},
			},
			"backup_target": schema.StringAttribute{
				MarkdownDescription: "Where its copies go: yet's bucket (`\"\"`) or the company's own (`company`) — " +
					"chosen in Yet's apps, as it takes the AWS key the company's Settings keep.",
				Computed:      true,
				PlanModifiers: keep,
			},
		},
	}
}

// backups is how its copies are to change: what was said and differs from
// what it had (nothing, on creation) — nothing at all for a database that is
// not copied, which Cloud would refuse.
func (m databaseModel) backups(was databaseModel) yet.BackupsInput {
	var in yet.BackupsInput
	if known(m.BackupEvery) && !m.BackupEvery.Equal(was.BackupEvery) {
		in.Every = intPtr(m.BackupEvery)
	}
	if known(m.BackupKeep) && !m.BackupKeep.Equal(was.BackupKeep) {
		in.Keep = intPtr(m.BackupKeep)
	}
	return in
}

// setBackups sets how a database is copied, when that changes, and reads it
// again: Cloud keeps the routine apart from the rest.
func (r *databaseResource) setBackups(ctx context.Context, db *yet.Database, in yet.BackupsInput) (*yet.Database, error) {
	if in == (yet.BackupsInput{}) {
		return db, nil
	}
	if _, err := r.client.Cloud.Databases.SetBackups(ctx, db.ID, in); err != nil {
		return nil, err
	}
	return r.client.Cloud.Databases.Get(ctx, db.ID)
}

func (m databaseModel) input(ctx context.Context, d *diagsSink) yet.DatabaseInput {
	return yet.DatabaseInput{
		Name:           strPtr(m.Name),
		Version:        strPtr(m.Version),
		MemoryMB:       intPtr(m.MemoryMB),
		Stopped:        boolPtr(m.Stopped),
		Features:       stringsOf(ctx, m.Features, &d.d),
		Hostname:       strPtr(m.Hostname),
		StudioRole:     strPtr(m.StudioRole),
		StudioReadOnly: boolPtr(m.StudioReadOnly),
	}
}

func (m *databaseModel) from(ctx context.Context, db *yet.Database, d *diagsSink) {
	m.ID = types.StringValue(db.ID)
	m.Kind = types.StringValue(db.Kind)
	m.Name = types.StringValue(db.Name)
	m.EnvironmentID = types.StringValue(db.EnvironmentID)
	m.Version = types.StringValue(db.Version)
	m.MemoryMB = types.Int64Value(int64(db.MemoryMB))
	m.Stopped = types.BoolValue(db.Stopped)
	m.Features = setOf(ctx, db.Features, &d.d)
	m.Hostname = types.StringValue(db.Hostname)
	m.StudioRole = types.StringValue(db.StudioRole)
	m.StudioReadOnly = types.BoolValue(db.StudioReadOnly)
	m.EnvVar = types.StringValue(db.EnvVar)
	m.Image = types.StringValue(db.Image)
	m.BackupEvery = types.Int64Value(int64(db.BackupEvery))
	m.BackupKeep = types.Int64Value(int64(db.BackupKeep))
	m.BackupTarget = types.StringValue(db.BackupTarget)
}

func (r *databaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan databaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var bucket *yet.BucketChoice
	if name := plan.BucketName.ValueString(); name != "" {
		bucket = &yet.BucketChoice{Name: name, Existing: plan.BucketExisting.ValueBool()}
	}
	db, err := r.client.Cloud.Databases.Create(ctx, plan.Kind.ValueString(), plan.Name.ValueString(), plan.EnvironmentID.ValueString(), bucket)
	if err != nil {
		failed(&resp.Diagnostics, "create the database", err)
		return
	}
	// What else it was given, set right after: Cloud makes a database with its defaults.
	d := &diagsSink{}
	if in := plan.input(ctx, d); in != (yet.DatabaseInput{Name: in.Name}) {
		if db, err = r.client.Cloud.Databases.Update(ctx, db.ID, in); err != nil {
			failed(&resp.Diagnostics, "set the database up", err)
			return
		}
	}
	if db, err = r.setBackups(ctx, db, plan.backups(databaseModel{})); err != nil {
		failed(&resp.Diagnostics, "set the database's backups", err)
		return
	}
	plan.from(ctx, db, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *databaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	db, err := r.client.Cloud.Databases.Get(ctx, state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the database", err)
		}
		return
	}
	d := &diagsSink{}
	state.from(ctx, db, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *databaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state databaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	db, err := r.client.Cloud.Databases.Update(ctx, plan.ID.ValueString(), plan.input(ctx, d))
	if err != nil {
		failed(&resp.Diagnostics, "change the database", err)
		return
	}
	if db, err = r.setBackups(ctx, db, plan.backups(state)); err != nil {
		failed(&resp.Diagnostics, "change the database's backups", err)
		return
	}
	plan.from(ctx, db, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *databaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if err := r.client.Cloud.Databases.Delete(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the database", err)
	}
}

func (r *databaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importID(ctx, req, resp)
}

// regionsOf is a list of regions as the API takes it, whole: none, Lambda off.
func regionsOf(l types.List) *[]string {
	out := []string{}
	for _, v := range l.Elements() {
		if s, ok := v.(types.String); ok {
			out = append(out, s.ValueString())
		}
	}
	return &out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// secretEnvOf is an application's variables from Vault, sent whole: none given, none.
func secretEnvOf(ctx context.Context, m types.Map, diags *diag.Diagnostics) *map[string]string {
	out := map[string]string{}
	if known(m) {
		diags.Append(m.ElementsAs(ctx, &out, false)...)
	}
	return &out
}
