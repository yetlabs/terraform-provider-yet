package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

var (
	keep    = []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	replace = []planmodifier.String{stringplanmodifier.RequiresReplace()}
	idAttr  = schema.StringAttribute{Computed: true, PlanModifiers: keep}
)

func cloudName() []validator.String {
	return []validator.String{stringvalidator.RegexMatches(kubeName, kubeNameRule)}
}

func importID(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ── yet_cloud_rack ───────────────────────────────────────────────────────────

type rackResource struct{ base }

func newRackResource() resource.Resource { return &rackResource{} }

type rackModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

func (r *rackResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_rack"
}

func (r *rackResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A rack: a group of servers and runtimes under a name of the company's — of no one kind: " +
			"where each server comes from (its `origin`) is the server's own, so one rack may hold machines of the company's " +
			"and, as they arrive, an AWS account's or a GCP project's.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr,
			"name": schema.StringAttribute{
				MarkdownDescription: "Its name, up to 60 characters: `Home Rack`.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 60)},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "What it is for, up to 300 characters.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
			},
		},
	}
}

func (m *rackModel) from(c *yet.Rack) {
	m.ID = types.StringValue(c.ID)
	m.Name = types.StringValue(c.Name)
	m.Description = types.StringValue(c.Description)
}

func (r *rackResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan rackModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := r.client.Cloud.Racks.Create(ctx, yet.RackInput{
		Name: strPtr(plan.Name), Description: strPtr(plan.Description),
	})
	if err != nil {
		failed(&resp.Diagnostics, "create the cloud", err)
		return
	}
	plan.from(c)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *rackResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state rackModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := r.client.Cloud.Racks.Get(ctx, state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the cloud", err)
		}
		return
	}
	state.from(c)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *rackResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan rackModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := r.client.Cloud.Racks.Update(ctx, plan.ID.ValueString(), yet.RackInput{
		Name: strPtr(plan.Name), Description: strPtr(plan.Description),
	})
	if err != nil {
		failed(&resp.Diagnostics, "change the cloud", err)
		return
	}
	plan.from(c)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *rackResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state rackModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if err := r.client.Cloud.Racks.Delete(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the cloud", err)
	}
}

func (r *rackResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importID(ctx, req, resp)
}

// ── yet_cloud_server ─────────────────────────────────────────────────────────

type serverResource struct{ base }

func newServerResource() resource.Resource { return &serverResource{} }

type serverModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	RackID        types.String `tfsdk:"rack_id"`
	Origin        types.String `tfsdk:"origin"`
	AWSAccountID  types.String `tfsdk:"aws_account_id"`
	Region        types.String `tfsdk:"region"`
	InstanceType  types.String `tfsdk:"instance_type"`
	DiskGB        types.Int64  `tfsdk:"disk_gb"`
	InstanceID    types.String `tfsdk:"instance_id"`
	Status        types.String `tfsdk:"status"`
	Hostname      types.String `tfsdk:"hostname"`
	OS            types.String `tfsdk:"os"`
	Arch          types.String `tfsdk:"arch"`
	JoinCommand   types.String `tfsdk:"join_command"`
	DockerCommand types.String `tfsdk:"docker_command"`
}

func (r *serverResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_server"
}

func (r *serverResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A machine of a local rack, which Yet's agent runs on. Made, it waits for its machine: " +
			"run `join_command` there — in a VM's user data, say — and it comes online.\n\n" +
			"-> **Note:** The join command's token works once, and for a while. It is made when the server is; " +
			"a machine that joins later needs a new one, from Yet's apps or the API.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr,
			"name": schema.StringAttribute{
				MarkdownDescription: "Its name: " + kubeNameRule + ".",
				Required:            true,
				Validators:          cloudName(),
			},
			"rack_id": schema.StringAttribute{
				MarkdownDescription: "The local rack it joins; the company's only one when left out.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()},
			},
			"origin": schema.StringAttribute{
				MarkdownDescription: "Where it comes from: `local`, a machine of the company's, or `aws`, an instance Cloud starts — " +
					"`aws` when `aws_account_id` is set.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"aws_account_id": schema.StringAttribute{
				MarkdownDescription: "An AWS account connected for Servers (`yet_settings_aws_account`'s `id`): Cloud starts an EC2 instance there, " +
					"which joins by itself as it boots; destroying the server ends it. Left out, a machine of the company's, joined with `join_command`.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "The instance's AWS region; the account's when left out.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()},
			},
			"instance_type": schema.StringAttribute{
				MarkdownDescription: "The instance's type, one Cloud offers (`t3.small`, `t4g.medium`, `m7i.large`…); `t3.small` when left out.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()},
			},
			"disk_gb": schema.Int64Attribute{
				MarkdownDescription: "Its disk, 20 to 1000 GB; 30 when left out.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.RequiresReplace(), int64planmodifier.UseStateForUnknown()},
			},
			"instance_id": schema.StringAttribute{
				MarkdownDescription: "The instance's id in EC2.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "`pending` until its agent first comes in; `online` or `offline` after.",
				Computed:            true,
			},
			"hostname": schema.StringAttribute{Computed: true, MarkdownDescription: "The machine's own name, as its agent says."},
			"os":       schema.StringAttribute{Computed: true, MarkdownDescription: "The machine's system: `linux`, `darwin`, `windows`."},
			"arch":     schema.StringAttribute{Computed: true, MarkdownDescription: "The machine's architecture: `amd64`, `arm64`."},
			"join_command": schema.StringAttribute{
				MarkdownDescription: "What to run on a Linux machine to join it: it checks Docker, then runs the agent. " +
					"Its token works once, and for a while; made when the server is.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: keep,
			},
			"docker_command": schema.StringAttribute{
				MarkdownDescription: "The same, as the `docker run` itself.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       keep,
			},
		},
	}
}

func (m *serverModel) from(s *yet.Server) {
	m.ID = types.StringValue(s.ID)
	m.Name = types.StringValue(s.Name)
	m.RackID = types.StringValue(s.RackID)
	m.Origin = types.StringValue(s.Origin)
	m.AWSAccountID = types.StringNull()
	if s.AWSAccountID != "" {
		m.AWSAccountID = types.StringValue(s.AWSAccountID)
	}
	m.Region = types.StringValue(s.Region)
	m.InstanceType = types.StringValue(s.InstanceType)
	m.DiskGB = types.Int64Value(int64(s.DiskGB))
	m.InstanceID = types.StringValue(s.InstanceID)
	m.Status = types.StringValue(s.Status)
	m.Hostname = types.StringValue(s.Hostname)
	m.OS = types.StringValue(s.OS)
	m.Arch = types.StringValue(s.Arch)
}

func (r *serverResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.AWSAccountID.IsNull() {
		s, err := r.client.Cloud.Servers.StartInAWS(ctx, yet.AWSServer{
			Name: plan.Name.ValueString(), RackID: plan.RackID.ValueString(), AWSAccountID: plan.AWSAccountID.ValueString(),
			Region: plan.Region.ValueString(), InstanceType: plan.InstanceType.ValueString(), DiskGB: int32(plan.DiskGB.ValueInt64()),
		})
		if err != nil {
			failed(&resp.Diagnostics, "start the server in AWS", err)
			return
		}
		plan.from(s)
		// It runs its join itself.
		plan.JoinCommand, plan.DockerCommand = types.StringValue(""), types.StringValue("")
		resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
		return
	}
	s, err := r.client.Cloud.Servers.Create(ctx, plan.Name.ValueString(), plan.RackID.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "create the server", err)
		return
	}
	plan.from(s)
	join, err := r.client.Cloud.Servers.Join(ctx, s.ID)
	if err != nil {
		failed(&resp.Diagnostics, "make the server's join command", err)
		join = &yet.Join{}
	}
	plan.JoinCommand = types.StringValue(join.Command)
	plan.DockerCommand = types.StringValue(join.Docker)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *serverResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.Cloud.Servers.Get(ctx, state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the server", err)
		}
		return
	}
	state.from(s)
	if state.JoinCommand.IsNull() {
		state.JoinCommand, state.DockerCommand = types.StringValue(""), types.StringValue("")
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *serverResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.Cloud.Servers.Rename(ctx, plan.ID.ValueString(), plan.Name.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "rename the server", err)
		return
	}
	plan.from(s)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *serverResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if err := r.client.Cloud.Servers.Delete(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the server", err)
	}
}

func (r *serverResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importID(ctx, req, resp)
}

// ── yet_cloud_runtime ────────────────────────────────────────────────────────

type runtimeResource struct{ base }

func newRuntimeResource() resource.Resource { return &runtimeResource{} }

type runtimeModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Stack          types.String `tfsdk:"stack"`
	ServerID       types.String `tfsdk:"server_id"`
	Ingress        types.String `tfsdk:"ingress"`
	RackID         types.String `tfsdk:"rack_id"`
	PoolID         types.String `tfsdk:"pool_id"`
	TunnelID       types.String `tfsdk:"tunnel_id"`
	Tunnel         types.Bool   `tfsdk:"tunnel"`
	IngressEnabled types.Bool   `tfsdk:"ingress_enabled"`
	Status         types.String `tfsdk:"status"`
}

func (r *runtimeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_runtime"
}

func (r *runtimeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "How things run on a server: k3s, Docker, or native. A k3s runtime goes behind " +
			"a Cloudflare tunnel of its own by itself, when Cloud's edge is set — `tunnel` says otherwise.\n\n" +
			"-> **Note:** Opening or closing its tunnel is a Cloud admin's, and changes its `ingress`: " +
			"its environments' records follow it.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr,
			"name": schema.StringAttribute{
				MarkdownDescription: "Its name: " + kubeNameRule + ".",
				Required:            true,
				Validators:          cloudName(),
			},
			"stack": schema.StringAttribute{
				MarkdownDescription: "How it runs: `local:traefik+k3s`, `local:docker` or `local:native`.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"server_id": schema.StringAttribute{
				MarkdownDescription: "The server it runs on.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"ingress": schema.StringAttribute{
				MarkdownDescription: "The address its environments' records point at: an IP or a name. Its tunnel's while it has one.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       keep,
			},
			"rack_id":   schema.StringAttribute{Computed: true, PlanModifiers: keep, MarkdownDescription: "Its server's rack."},
			"pool_id":   schema.StringAttribute{Computed: true, MarkdownDescription: "The pool it is a member of, if any — set on yet_pool."},
			"tunnel_id": schema.StringAttribute{Computed: true, MarkdownDescription: "Its Cloudflare tunnel, when it has one."},
			"tunnel": schema.BoolAttribute{
				MarkdownDescription: "Whether it is behind a Cloudflare tunnel of its own — its edge. A k3s runtime is, " +
					"made while Cloud's edge is set, unless this says otherwise; only k3s runtimes have one, for now.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"ingress_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether a k3s runtime's ingress is on: on unless set to `false` — for one that runs " +
					"runners only. Off, it takes no environment and joins no pool; turned off or on, the agent makes its " +
					"k3s again, its data kept — and not off while an environment runs there.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{Computed: true, MarkdownDescription: "How it stands, as Cloud shows it."},
		},
	}
}

func (m *runtimeModel) from(rt *yet.Runtime) {
	m.ID = types.StringValue(rt.ID)
	m.Name = types.StringValue(rt.Name)
	m.Stack = types.StringValue(rt.Stack)
	m.ServerID = types.StringValue(rt.ServerID)
	m.Ingress = types.StringValue(rt.Ingress)
	m.RackID = types.StringValue(rt.RackID)
	m.PoolID = types.StringValue(rt.PoolID)
	m.TunnelID = types.StringValue(rt.TunnelID)
	m.Tunnel = types.BoolValue(rt.TunnelID != "")
	m.IngressEnabled = types.BoolValue(rt.IngressEnabled)
	m.Status = types.StringValue(rt.Status)
}

// tunnel opens or closes a runtime's tunnel, when it is not as said: the
// runtime as it is after.
func (r *runtimeResource) tunnel(ctx context.Context, rt *yet.Runtime, want types.Bool) (*yet.Runtime, error) {
	if !known(want) || want.ValueBool() == (rt.TunnelID != "") {
		return rt, nil
	}
	if want.ValueBool() {
		return r.client.Cloud.Runtimes.OpenTunnel(ctx, rt.ID)
	}
	return r.client.Cloud.Runtimes.CloseTunnel(ctx, rt.ID)
}

// ModifyPlan leaves its ingress to be known after apply when its tunnel
// opens or closes, and no ingress was said: the tunnel's name comes, or goes.
func (r *runtimeResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state, config runtimeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if known(plan.Tunnel) && !plan.Tunnel.Equal(state.Tunnel) && config.Ingress.IsNull() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("ingress"), types.StringUnknown())...)
	}
}

func (r *runtimeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan runtimeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rt, err := r.client.Cloud.Runtimes.Create(ctx, yet.RuntimeInput{
		Name: strPtr(plan.Name), Stack: plan.Stack.ValueString(), ServerID: plan.ServerID.ValueString(),
		IngressEnabled: boolPtr(plan.IngressEnabled),
	})
	if err != nil {
		failed(&resp.Diagnostics, "create the runtime", err)
		return
	}
	if ingress := strPtr(plan.Ingress); ingress != nil && *ingress != rt.Ingress {
		if rt, err = r.client.Cloud.Runtimes.Update(ctx, rt.ID, yet.RuntimeInput{Ingress: ingress}); err != nil {
			failed(&resp.Diagnostics, "set the runtime's ingress", err)
			return
		}
	}
	if rt, err = r.tunnel(ctx, rt, plan.Tunnel); err != nil {
		failed(&resp.Diagnostics, "open or close the runtime's tunnel", err)
		return
	}
	plan.from(rt)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *runtimeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state runtimeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rt, err := r.client.Cloud.Runtimes.Get(ctx, state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the runtime", err)
		}
		return
	}
	state.from(rt)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *runtimeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state runtimeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := yet.RuntimeInput{Name: strPtr(plan.Name)}
	if known(plan.Ingress) && !plan.Ingress.Equal(state.Ingress) {
		in.Ingress = strPtr(plan.Ingress)
	}
	if known(plan.IngressEnabled) && !plan.IngressEnabled.Equal(state.IngressEnabled) {
		in.IngressEnabled = boolPtr(plan.IngressEnabled)
	}
	rt, err := r.client.Cloud.Runtimes.Update(ctx, plan.ID.ValueString(), in)
	if err != nil {
		failed(&resp.Diagnostics, "change the runtime", err)
		return
	}
	if rt, err = r.tunnel(ctx, rt, plan.Tunnel); err != nil {
		failed(&resp.Diagnostics, "open or close the runtime's tunnel", err)
		return
	}
	plan.from(rt)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *runtimeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state runtimeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if err := r.client.Cloud.Runtimes.Delete(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the runtime", err)
	}
}

func (r *runtimeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importID(ctx, req, resp)
}

// ── yet_cloud_pool ───────────────────────────────────────────────────────────

type poolResource struct{ base }

func newPoolResource() resource.Resource { return &poolResource{} }

type poolModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Color       types.String `tfsdk:"color"`
	RuntimeIDs  types.Set    `tfsdk:"runtime_ids"`
	Move        types.Bool   `tfsdk:"move_environments"`
	TunnelID    types.String `tfsdk:"tunnel_id"`
	Tunnel      types.Bool   `tfsdk:"tunnel"`
}

func (r *poolResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_pool"
}

func (r *poolResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Runtimes that run the same environments together — across servers, or clouds.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr,
			"name": schema.StringAttribute{
				MarkdownDescription: "Its name: " + kubeNameRule + ".",
				Required:            true,
				Validators:          cloudName(),
			},
			"description": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				MarkdownDescription: "What it is for.",
			},
			"color": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("gray"),
				MarkdownDescription: "How Cloud draws it: `" + joinTicks(colors) + "`.",
				Validators:          []validator.String{stringvalidator.OneOf(colors...)},
			},
			"runtime_ids": schema.SetAttribute{
				MarkdownDescription: "Its members.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"move_environments": schema.BoolAttribute{
				MarkdownDescription: "Whether the environments of runtimes joining move onto the pool, or stay theirs. " +
					"Cloud asks when a joining runtime has some.",
				Optional: true,
			},
			"tunnel_id": schema.StringAttribute{Computed: true, MarkdownDescription: "Its Cloudflare tunnel, when it has one."},
			"tunnel": schema.BoolAttribute{
				MarkdownDescription: "Whether it is behind a Cloudflare tunnel of its own — its edge, for every member — " +
					"or its members are each reached on their own. Opening or closing it is a Cloud admin's.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// tunnel opens or closes a pool's tunnel, when it is not as said.
func (r *poolResource) tunnel(ctx context.Context, p *yet.Pool, want types.Bool) (*yet.Pool, error) {
	if !known(want) || want.ValueBool() == (p.TunnelID != "") {
		return p, nil
	}
	if want.ValueBool() {
		return r.client.Cloud.Pools.OpenTunnel(ctx, p.ID)
	}
	return r.client.Cloud.Pools.CloseTunnel(ctx, p.ID)
}

func (r *poolResource) input(ctx context.Context, plan poolModel, diags *diag.Diagnostics) yet.PoolInput {
	return yet.PoolInput{
		Name:        strPtr(plan.Name),
		Description: strPtr(plan.Description),
		Color:       strPtr(plan.Color),
		RuntimeIDs:  stringsOf(ctx, plan.RuntimeIDs, diags),
		Move:        boolPtr(plan.Move),
	}
}

func (m *poolModel) from(ctx context.Context, p *yet.Pool, diags *diagsSink) {
	m.ID = types.StringValue(p.ID)
	m.Name = types.StringValue(p.Name)
	m.Description = types.StringValue(p.Description)
	m.Color = types.StringValue(p.Color)
	m.TunnelID = types.StringValue(p.TunnelID)
	m.Tunnel = types.BoolValue(p.TunnelID != "")
	ids := make([]string, 0, len(p.Members))
	for _, rt := range p.Members {
		ids = append(ids, rt.ID)
	}
	m.RuntimeIDs = setOf(ctx, ids, &diags.d)
}

func (r *poolResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan poolModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.client.Cloud.Pools.Create(ctx, r.input(ctx, plan, &resp.Diagnostics))
	if err != nil {
		failed(&resp.Diagnostics, "create the pool", err)
		return
	}
	if p, err = r.tunnel(ctx, p, plan.Tunnel); err != nil {
		failed(&resp.Diagnostics, "open or close the pool's tunnel", err)
		return
	}
	d := &diagsSink{}
	plan.from(ctx, p, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *poolResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state poolModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.client.Cloud.Pools.Get(ctx, state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the pool", err)
		}
		return
	}
	d := &diagsSink{}
	state.from(ctx, p, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *poolResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan poolModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.client.Cloud.Pools.Update(ctx, plan.ID.ValueString(), r.input(ctx, plan, &resp.Diagnostics))
	if err != nil {
		failed(&resp.Diagnostics, "change the pool", err)
		return
	}
	if p, err = r.tunnel(ctx, p, plan.Tunnel); err != nil {
		failed(&resp.Diagnostics, "open or close the pool's tunnel", err)
		return
	}
	d := &diagsSink{}
	plan.from(ctx, p, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *poolResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state poolModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if err := r.client.Cloud.Pools.Delete(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the pool", err)
	}
}

func (r *poolResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importID(ctx, req, resp)
}
