package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

/*
Data sources find what is there by its name — an environment, a runtime, a
zone made in yet's apps — for resources to refer to by id.
*/

func byName[T any](list []T, name string, nameOf func(T) string) (*T, bool) {
	for _, x := range list {
		if nameOf(x) == name {
			return &x, true
		}
	}
	return nil, false
}

func computed(desc string) schema.StringAttribute {
	return schema.StringAttribute{MarkdownDescription: desc, Computed: true}
}

func named(what string) schema.StringAttribute {
	return schema.StringAttribute{MarkdownDescription: "The " + what + "'s name.", Required: true}
}

// ── data "yet_cloud_environment" ─────────────────────────────────────────────

type environmentDataSource struct{ dataBase }

func newEnvironmentDataSource() datasource.DataSource { return &environmentDataSource{} }

func (d *environmentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_environment"
}

func (d *environmentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An environment, by its name.",
		Attributes: map[string]schema.Attribute{
			"name":           named("environment"),
			"id":             computed("Its id."),
			"color":          computed("How Cloud draws it."),
			"runtime_id":     computed("The runtime it runs on, if on one."),
			"pool_id":        computed("The pool it runs on, if on one."),
			"zone_id":        computed("The DNS zone it answers in, if any."),
			"prefix":         computed("What goes before the zone's name."),
			"domain":         computed("Where its applications answer."),
			"aws_account_id": computed("The AWS account its applications' buckets are made in, if set."),
		},
	}
}

func (d *environmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m environmentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	all, err := d.client.Cloud.Environments.List(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "list the environments", err)
		return
	}
	e, ok := byName(all, m.Name.ValueString(), func(e yet.Environment) string { return e.Name })
	if !ok {
		resp.Diagnostics.AddError("No such environment", "No environment named "+m.Name.ValueString()+" that the key can see.")
		return
	}
	m.from(e)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// ── data "yet_cloud_runtime" ─────────────────────────────────────────────────

type runtimeDataSource struct{ dataBase }

func newRuntimeDataSource() datasource.DataSource { return &runtimeDataSource{} }

func (d *runtimeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_runtime"
}

func (d *runtimeDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A runtime, by its name — and its server's, when another server has one of that name.",
		Attributes: map[string]schema.Attribute{
			"name":  named("runtime"),
			"id":    computed("Its id."),
			"stack": computed("How it runs."),
			"server_id": schema.StringAttribute{
				MarkdownDescription: "The server it runs on: say it when runtimes of that name are on several.",
				Optional:            true,
				Computed:            true,
			},
			"ingress":   computed("The address its environments' records point at."),
			"rack_id":   computed("Its server's rack."),
			"pool_id":   computed("The pool it is a member of, if any."),
			"tunnel_id": computed("Its Cloudflare tunnel, if any."),
			"tunnel":    schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether it is behind a Cloudflare tunnel of its own."},
			"ingress_enabled": schema.BoolAttribute{
				Computed: true, MarkdownDescription: "Whether a k3s runtime's ingress is on: off for one that runs runners only.",
			},
			"status": computed("How it stands."),
		},
	}
}

func (d *runtimeDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m runtimeModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	all, err := d.client.Cloud.Runtimes.List(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "list the runtimes", err)
		return
	}
	var found []yet.Runtime
	for _, r := range all {
		if r.Name == m.Name.ValueString() && (!known(m.ServerID) || r.ServerID == m.ServerID.ValueString()) {
			found = append(found, r)
		}
	}
	switch len(found) {
	case 0:
		resp.Diagnostics.AddError("No such runtime", "No runtime named "+m.Name.ValueString()+".")
		return
	case 1:
	default:
		resp.Diagnostics.AddError("Runtimes of that name on several servers",
			"More than one server has a runtime named "+m.Name.ValueString()+": say which, with server_id.")
		return
	}
	m.from(&found[0])
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// ── data "yet_cloud_dns_zone" ────────────────────────────────────────────────

type dnsZoneDataSource struct{ dataBase }

func newDNSZoneDataSource() datasource.DataSource { return &dnsZoneDataSource{} }

func (d *dnsZoneDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_dns_zone"
}

func (d *dnsZoneDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A DNS zone, by its domain.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{MarkdownDescription: "The domain: `acme.dev`.", Required: true},
			"id":   computed("Its id."),
			"nameservers": schema.ListAttribute{
				MarkdownDescription: "Where the domain's registrar has to point.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"status": computed("`pending` or `active`."),
		},
	}
}

func (d *dnsZoneDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m dnsZoneModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	all, err := d.client.Cloud.DNS.Zones(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "list the zones", err)
		return
	}
	z, ok := byName(all, m.Name.ValueString(), func(z yet.Zone) string { return z.Name })
	if !ok {
		resp.Diagnostics.AddError("No such zone", "No zone for "+m.Name.ValueString()+".")
		return
	}
	sink := &diagsSink{}
	m.from(ctx, z, sink)
	resp.Diagnostics.Append(sink.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// ── data "yet_cloud_stacks" ──────────────────────────────────────────────────

type stacksDataSource struct{ dataBase }

func newStacksDataSource() datasource.DataSource { return &stacksDataSource{} }

type stacksModel struct {
	Stacks []stackModel `tfsdk:"stacks"`
}

type stackModel struct {
	ID           types.String `tfsdk:"id"`
	Kind         types.String `tfsdk:"kind"`
	Runtime      types.String `tfsdk:"runtime"`
	Ingress      types.String `tfsdk:"ingress"`
	Label        types.String `tfsdk:"label"`
	Description  types.String `tfsdk:"description"`
	Capabilities types.List   `tfsdk:"capabilities"`
	Available    types.Bool   `tfsdk:"available"`
}

func (d *stacksDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_stacks"
}

func (d *stacksDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every way a runtime can be — where, on what, and how traffic gets in: what a " +
			"`yet_cloud_runtime`'s `stack` takes.",
		Attributes: map[string]schema.Attribute{
			"stacks": schema.ListNestedAttribute{
				MarkdownDescription: "Each stack.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          computed("What a runtime's `stack` takes: `local:traefik+k3s`."),
						"kind":        computed("Where it runs: `local`, `aws`, `gcp`."),
						"runtime":     computed("What runs things: `k3s`, `docker`, `native`, `ecs`…"),
						"ingress":     computed("How traffic gets in: `traefik`, `alb`… — empty for none."),
						"label":       computed("Its name, as Cloud shows it."),
						"description": computed("What it is for."),
						"capabilities": schema.ListAttribute{
							MarkdownDescription: "What a runtime of it can do.",
							ElementType:         types.StringType,
							Computed:            true,
						},
						"available": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether it can be made now."},
					},
				},
			},
		},
	}
}

func (d *stacksDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	all, err := d.client.Cloud.Stacks(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "list the stacks", err)
		return
	}
	m := stacksModel{Stacks: []stackModel{}}
	for _, s := range all {
		m.Stacks = append(m.Stacks, stackModel{
			ID: types.StringValue(s.ID), Kind: types.StringValue(s.Kind), Runtime: types.StringValue(s.Runtime),
			Ingress: types.StringValue(s.Ingress), Label: types.StringValue(s.Label), Description: types.StringValue(s.Description),
			Capabilities: listOf(ctx, s.Capabilities, &resp.Diagnostics), Available: types.BoolValue(s.Available),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
