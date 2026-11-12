package provider

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

/*
What a runtime keeps besides applications: a Docker runtime's containers,
a native one's GitHub runners. Both are a runtime's, so they are imported as
<runtime_id>/<id>, and read from their runtime's list — Cloud serves neither
one at a time.
*/

var (
	noStrings  = types.ListValueMust(types.StringType, []attr.Value{})
	noStringsS = types.SetValueMust(types.StringType, []attr.Value{})
	noValues   = types.MapValueMust(types.StringType, map[string]attr.Value{})

	// runnerName is a runner's: lowercase letters, digits and dashes, up to 32.
	runnerName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)
	envVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

// envKeys are a container's variables' names: letters, digits and _.
var envKeys = []validator.Map{mapvalidator.KeysAre(stringvalidator.RegexMatches(envVarName, "letters, digits and _, not starting with a digit"))}

// importOfRuntime takes "<runtime_id>/<id>", for what a runtime keeps.
func importOfRuntime(what string) func(context.Context, resource.ImportStateRequest, *resource.ImportStateResponse) {
	return func(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
		runtime, id, ok := strings.Cut(req.ID, "/")
		if !ok || runtime == "" || id == "" {
			resp.Diagnostics.AddError("Unexpected import id", "Import a "+what+" as <runtime_id>/<"+what+"_id>.")
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("runtime_id"), runtime)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	}
}

// ── yet_cloud_container ──────────────────────────────────────────────────────

type containerResource struct{ base }

func newContainerResource() resource.Resource { return &containerResource{} }

type containerModel struct {
	ID         types.String `tfsdk:"id"`
	RuntimeID  types.String `tfsdk:"runtime_id"`
	Name       types.String `tfsdk:"name"`
	Image      types.String `tfsdk:"image"`
	Command    types.List   `tfsdk:"command"`
	Env        types.Map    `tfsdk:"env"`
	Secrets    types.Map    `tfsdk:"secrets"`
	Privileged types.Bool   `tfsdk:"privileged"`
	Volumes    types.List   `tfsdk:"volumes"`
	Replicas   types.Int64  `tfsdk:"replicas"`
	Enabled    types.Bool   `tfsdk:"enabled"`
	State      types.String `tfsdk:"state"`
	Message    types.String `tfsdk:"message"`
}

func (r *containerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_container"
}

func (r *containerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A container a Docker runtime keeps running, plainly — no orchestrator, no ingress: " +
			"CI runners and the Docker they build in, a tool the team keeps up. Its runtime's agent makes it again " +
			"whenever its definition changes.\n\n" +
			"-> **Note:** Only Cloud admins define containers: one may be privileged, and runs on a machine of the " +
			"company's with Docker's own powers.\n\n" +
			"~> **Note:** A secret's value is sealed in Cloud and never shown again: the state keeps what was said, " +
			"and an imported container's secrets are empty until set.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr,
			"runtime_id": schema.StringAttribute{
				MarkdownDescription: "The Docker runtime (`local:docker`) it runs on.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Its name on the runtime: " + kubeNameRule + ".",
				Required:            true,
				Validators:          cloudName(),
			},
			"image": schema.StringAttribute{
				MarkdownDescription: "The image it runs: `docker:27-dind`.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^\S+$`), "an image, with no spaces")},
			},
			"command": schema.ListAttribute{
				MarkdownDescription: "What it runs, in place of its image's own.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Default:             listdefault.StaticValue(noStrings),
			},
			"env": schema.MapAttribute{
				MarkdownDescription: "Its environment, by name.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Default:             mapdefault.StaticValue(noValues),
				Validators:          envKeys,
			},
			"secrets": schema.MapAttribute{
				MarkdownDescription: "Its environment's secrets, by name: sealed in Cloud, never shown again.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Sensitive:           true,
				Default:             mapdefault.StaticValue(noValues),
				Validators:          envKeys,
			},
			"privileged": schema.BoolAttribute{
				MarkdownDescription: "Whether it runs privileged — as Docker in Docker must. `false` unless set.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"volumes": schema.ListAttribute{
				MarkdownDescription: "Named volumes, kept across its recreations: `cache:/var/lib/docker`.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Default:             listdefault.StaticValue(noStrings),
			},
			"replicas": schema.Int64Attribute{
				MarkdownDescription: "How many alike it runs as — CI runners, each taking a job: 1 to 20, `1` unless set.",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(1),
				Validators:          []validator.Int64{int64validator.Between(1, 20)},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether it runs. Turned off, the agent takes it away — its definition kept, secrets and all.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"state": schema.StringAttribute{
				MarkdownDescription: "How it is, as the runtime's agent last said: `pending`, `running`, `disabled`…",
				Computed:            true,
			},
			"message": schema.StringAttribute{
				MarkdownDescription: "What the agent last said of it, when something is wrong.",
				Computed:            true,
			},
		},
	}
}

func (m containerModel) input(ctx context.Context, d *diagsSink) yet.ContainerInput {
	env := map[string]string{}
	secrets := map[string]string{}
	if known(m.Env) {
		d.d.Append(m.Env.ElementsAs(ctx, &env, false)...)
	}
	if known(m.Secrets) {
		d.d.Append(m.Secrets.ElementsAs(ctx, &secrets, false)...)
	}
	vars := []yet.EnvVar{}
	for _, name := range sortedKeys(env) {
		vars = append(vars, yet.EnvVar{Name: name, Value: env[name]})
	}
	for _, name := range sortedKeys(secrets) {
		vars = append(vars, yet.EnvVar{Name: name, Value: secrets[name], Secret: true})
	}
	in := yet.ContainerInput{
		Name: m.Name.ValueString(), Image: m.Image.ValueString(), Env: vars,
		Privileged: m.Privileged.ValueBool(), Replicas: int(m.Replicas.ValueInt64()),
		Command: []string{}, Volumes: []string{},
	}
	if c := stringsOf(ctx, m.Command, &d.d); c != nil {
		in.Command = *c
	}
	if v := stringsOf(ctx, m.Volumes, &d.d); v != nil {
		in.Volumes = *v
	}
	return in
}

// from is the container as Cloud has it; its secrets' values are the ones
// said before, as Cloud never shows them back.
func (m *containerModel) from(ctx context.Context, ct *yet.Container, d *diagsSink) {
	said := map[string]string{}
	if known(m.Secrets) {
		d.d.Append(m.Secrets.ElementsAs(ctx, &said, false)...)
	}
	env, secrets := map[string]string{}, map[string]string{}
	for _, v := range ct.Env {
		if v.Secret {
			secrets[v.Name] = said[v.Name]
		} else {
			env[v.Name] = v.Value
		}
	}
	m.ID = types.StringValue(ct.ID)
	m.RuntimeID = types.StringValue(ct.RuntimeID)
	m.Name = types.StringValue(ct.Name)
	m.Image = types.StringValue(ct.Image)
	m.Command = listOf(ctx, ct.Command, &d.d)
	m.Env = mapOf(ctx, env, &d.d)
	m.Secrets = mapOf(ctx, secrets, &d.d)
	m.Privileged = types.BoolValue(ct.Privileged)
	m.Volumes = listOf(ctx, ct.Volumes, &d.d)
	m.Replicas = types.Int64Value(int64(max(ct.Replicas, 1)))
	m.Enabled = types.BoolValue(!ct.Disabled)
	m.State = types.StringValue(ct.State)
	m.Message = types.StringValue(ct.Message)
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (r *containerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan containerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	in := plan.input(ctx, d)
	if resp.Diagnostics.Append(d.d...); resp.Diagnostics.HasError() {
		return
	}
	runtime := plan.RuntimeID.ValueString()
	ct, err := r.client.Cloud.Runtimes.CreateContainer(ctx, runtime, in)
	if err != nil {
		failed(&resp.Diagnostics, "create the container", err)
		return
	}
	// Made on; turned off right after when it is said to be.
	if !plan.Enabled.ValueBool() {
		if ct, err = r.client.Cloud.Runtimes.EnableContainer(ctx, runtime, ct.ID, false); err != nil {
			failed(&resp.Diagnostics, "turn the container off", err)
			return
		}
	}
	plan.from(ctx, ct, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *containerResource) find(ctx context.Context, runtime, id string) (*yet.Container, error) {
	all, err := r.client.Cloud.Runtimes.Containers(ctx, runtime)
	if err != nil {
		return nil, err
	}
	for _, ct := range all {
		if ct.ID == id {
			return &ct, nil
		}
	}
	return nil, nil
}

func (r *containerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state containerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ct, err := r.find(ctx, state.RuntimeID.ValueString(), state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the container", err)
		}
		return
	}
	if ct == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	d := &diagsSink{}
	state.from(ctx, ct, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *containerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state containerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	in := plan.input(ctx, d)
	if resp.Diagnostics.Append(d.d...); resp.Diagnostics.HasError() {
		return
	}
	runtime, id := plan.RuntimeID.ValueString(), plan.ID.ValueString()
	ct, err := r.client.Cloud.Runtimes.UpdateContainer(ctx, runtime, id, in)
	if err != nil {
		failed(&resp.Diagnostics, "change the container", err)
		return
	}
	if plan.Enabled.ValueBool() == ct.Disabled {
		if ct, err = r.client.Cloud.Runtimes.EnableContainer(ctx, runtime, id, plan.Enabled.ValueBool()); err != nil {
			failed(&resp.Diagnostics, "turn the container on or off", err)
			return
		}
	}
	plan.from(ctx, ct, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *containerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state containerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	err := r.client.Cloud.Runtimes.DeleteContainer(ctx, state.RuntimeID.ValueString(), state.ID.ValueString())
	if err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the container", err)
	}
}

// ImportState takes "<runtime_id>/<container_id>".
func (r *containerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importOfRuntime("container")(ctx, req, resp)
}

// ── yet_cloud_runner ─────────────────────────────────────────────────────────

type runnerResource struct{ base }

func newRunnerResource() resource.Resource { return &runnerResource{} }

type runnerModel struct {
	ID          types.String `tfsdk:"id"`
	RuntimeID   types.String `tfsdk:"runtime_id"`
	Name        types.String `tfsdk:"name"`
	URL         types.String `tfsdk:"url"`
	Labels      types.Set    `tfsdk:"labels"`
	Replicas    types.Int64  `tfsdk:"replicas"`
	MaxReplicas types.Int64  `tfsdk:"max_replicas"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	GitHubToken types.String `tfsdk:"github_token"`
	HasToken    types.Bool   `tfsdk:"has_token"`
	Problem     types.String `tfsdk:"problem"`
}

func (r *runnerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_runner"
}

func (r *runnerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "GitHub Actions runners kept by a runtime: a native one — a Mac or Windows, whose agent runs " +
			"as its own `yetd` account — for the builds only that system makes; or a k3s one, on Linux or in Windows's WSL, each copy a pod " +
			"sandboxed by gVisor, with a Docker of its own for `docker run`, `build` and `compose`, nothing privileged " +
			"on the machine. Each copy takes one job and starts again clean. Cloud asks GitHub for their registration " +
			"tokens, a new one before each expires; the machine never holds the company's token.\n\n" +
			"-> **Note:** Cloud asks GitHub with the runner's own `github_token`, or, without one, with the company's " +
			"GitHub connection — `yet_settings_connection_github`. Either must be able to manage the repository's or the " +
			"organization's runners: a fine-grained token with *Administration: read and write* on the repository, " +
			"or *Self-hosted runners* on the organization.\n\n" +
			"~> **Note:** Only Cloud admins define runners: a job runs on the company's machine, so they are for the " +
			"company's own repositories.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr,
			"runtime_id": schema.StringAttribute{
				MarkdownDescription: "The runtime they run on: a native one (`local:native`, a Mac or Windows), or a k3s one " +
					"(`local:traefik+k3s`), on Linux or Windows.",
				Required:      true,
				PlanModifiers: replace,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Its name on the runtime: lowercase letters, digits and dashes, up to 32.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(runnerName, "lowercase letters, digits and dashes, up to 32")},
			},
			"url": schema.StringAttribute{
				MarkdownDescription: "What it runs for, on GitHub: an organization, `https://github.com/acme`, or a repository, " +
					"`https://github.com/acme/ios`.",
				Required: true,
				Validators: []validator.String{stringvalidator.RegexMatches(
					regexp.MustCompile(`^https://github\.com/[^/\s]+(/[^/\s]+)?/?$`), "https://github.com/<organization>[/<repository>]")},
			},
			"labels": schema.SetAttribute{
				MarkdownDescription: "Its labels, for workflows' `runs-on`: letters, digits, dots, dashes and underscores.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Default:             setdefault.StaticValue(noStringsS),
			},
			"replicas": schema.Int64Attribute{
				MarkdownDescription: "How many copies run, each taking a job: 1 to 20, `1` unless set. Growing " +
					"(`max_replicas`), the fewest always there — `0` too.",
				Optional:   true,
				Computed:   true,
				Default:    int64default.StaticInt64(1),
				Validators: []validator.Int64{int64validator.Between(0, 20)},
			},
			"max_replicas": schema.Int64Attribute{
				MarkdownDescription: "On a k3s runtime, up to how many copies it grows, one a job GitHub queues for it — " +
					"from `replicas` up to this, back down as the jobs complete; a copy busy with a job is never stopped. " +
					"`0`, unless set, keeps it at `replicas`.\n\n" +
					"  Cloud hears of the jobs through its GitHub App's `workflow_job` events: the company's GitHub must be " +
					"connected as the App (`yet_settings_connection_github`), not with a token.",
				Optional:   true,
				Computed:   true,
				Default:    int64default.StaticInt64(0),
				Validators: []validator.Int64{int64validator.Between(0, 20)},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether it runs. `true` unless set.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"github_token": schema.StringAttribute{
				MarkdownDescription: "A GitHub token of its own; the company's GitHub connection is used without one. " +
					"Sealed in Cloud, never shown again — taken away from the config, the one given stays.",
				Optional:  true,
				Sensitive: true,
			},
			"has_token": schema.BoolAttribute{
				MarkdownDescription: "Whether it has a GitHub token of its own.",
				Computed:            true,
			},
			"problem": schema.StringAttribute{
				MarkdownDescription: "What GitHub said, when it would not register it.",
				Computed:            true,
			},
		},
	}
}

func (m runnerModel) input(ctx context.Context, d *diagsSink) yet.RunnerInput {
	in := yet.RunnerInput{
		Name: m.Name.ValueString(), URL: m.URL.ValueString(), Labels: []string{},
		Replicas: int(m.Replicas.ValueInt64()), MaxReplicas: int(m.MaxReplicas.ValueInt64()), Disabled: !m.Enabled.ValueBool(),
		GitHubToken: m.GitHubToken.ValueString(),
	}
	if l := stringsOf(ctx, m.Labels, &d.d); l != nil {
		in.Labels = *l
	}
	return in
}

func (m *runnerModel) from(ctx context.Context, rn *yet.Runner, d *diagsSink) {
	m.ID = types.StringValue(rn.ID)
	m.RuntimeID = types.StringValue(rn.RuntimeID)
	m.Name = types.StringValue(rn.Name)
	// Cloud keeps it with no final slash: as it was said while it means the same.
	if strings.TrimRight(m.URL.ValueString(), "/") != rn.URL {
		m.URL = types.StringValue(rn.URL)
	}
	m.Labels = setOf(ctx, rn.Labels, &d.d)
	m.Replicas = types.Int64Value(int64(rn.Replicas))
	m.MaxReplicas = types.Int64Value(int64(rn.MaxReplicas))
	m.Enabled = types.BoolValue(!rn.Disabled)
	m.HasToken = types.BoolValue(rn.HasToken)
	m.Problem = types.StringValue(rn.Problem)
}

func (r *runnerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan runnerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	in := plan.input(ctx, d)
	if resp.Diagnostics.Append(d.d...); resp.Diagnostics.HasError() {
		return
	}
	rn, err := r.client.Cloud.Runtimes.CreateRunner(ctx, plan.RuntimeID.ValueString(), in)
	if err != nil {
		failed(&resp.Diagnostics, "create the runner", err)
		return
	}
	plan.from(ctx, rn, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *runnerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state runnerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	all, err := r.client.Cloud.Runtimes.Runners(ctx, state.RuntimeID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the runner", err)
		}
		return
	}
	for _, rn := range all {
		if rn.ID == state.ID.ValueString() {
			d := &diagsSink{}
			state.from(ctx, &rn, d)
			resp.Diagnostics.Append(d.d...)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *runnerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state runnerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	in := plan.input(ctx, d)
	if resp.Diagnostics.Append(d.d...); resp.Diagnostics.HasError() {
		return
	}
	// The token only when it changed: sent again, it would make the runner
	// register anew for nothing.
	if plan.GitHubToken.Equal(state.GitHubToken) {
		in.GitHubToken = ""
	}
	rn, err := r.client.Cloud.Runtimes.UpdateRunner(ctx, plan.RuntimeID.ValueString(), plan.ID.ValueString(), in)
	if err != nil {
		failed(&resp.Diagnostics, "change the runner", err)
		return
	}
	plan.from(ctx, rn, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *runnerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state runnerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	err := r.client.Cloud.Runtimes.DeleteRunner(ctx, state.RuntimeID.ValueString(), state.ID.ValueString())
	if err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the runner", err)
	}
}

// ImportState takes "<runtime_id>/<runner_id>".
func (r *runnerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importOfRuntime("runner")(ctx, req, resp)
}
