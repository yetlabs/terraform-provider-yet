package provider

import (
	"cmp"
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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

// nameOrID are a data source's name and id when either finds it: one of them said, the other read.
func nameOrID(name string) (schema.StringAttribute, schema.StringAttribute) {
	return schema.StringAttribute{
			MarkdownDescription: name + "; or `id`.",
			Optional:            true,
			Computed:            true,
			Validators:          []validator.String{stringvalidator.ExactlyOneOf(path.MatchRoot("name"), path.MatchRoot("id"))},
		},
		schema.StringAttribute{MarkdownDescription: "Its id; or `name`.", Optional: true, Computed: true}
}

// ── data "yet_cloud_environment" ─────────────────────────────────────────────

type environmentDataSource struct{ dataBase }

func newEnvironmentDataSource() datasource.DataSource { return &environmentDataSource{} }

func (d *environmentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_environment"
}

func (d *environmentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	name, id := nameOrID("Its name")
	resp.Schema = schema.Schema{
		MarkdownDescription: "An environment, by its name or its id.",
		Attributes: map[string]schema.Attribute{
			"name":           name,
			"id":             id,
			"color":          computed("How Cloud draws it."),
			"runtime_id":     computed("The runtime it runs on, if on one."),
			"pool_id":        computed("The pool it runs on, if on one."),
			"zone_id":        computed("The DNS zone it answers in, if any."),
			"prefix":         computed("What goes before the zone's name."),
			"domain":         computed("Where its applications answer."),
			"aws_account_id": computed("The AWS account its applications' buckets are made in, if set."),
			"functions": schema.ListAttribute{
				MarkdownDescription: "The Lambda runtimes beside its runtimes its applications may run on as functions.",
				ElementType:         types.StringType,
				Computed:            true,
			},
		},
	}
}

func (d *environmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m environmentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !m.ID.IsNull() {
		e, err := d.client.Cloud.Environments.Get(ctx, m.ID.ValueString())
		if err != nil {
			failed(&resp.Diagnostics, "read the environment", err)
			return
		}
		m.from(e)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
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

// ── data "yet_cloud_namespace" ───────────────────────────────────────────────

type namespaceDataSource struct{ dataBase }

func newNamespaceDataSource() datasource.DataSource { return &namespaceDataSource{} }

func (d *namespaceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_namespace"
}

func (d *namespaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	name, id := nameOrID("Its name")
	resp.Schema = schema.Schema{
		MarkdownDescription: "A namespace — a group of applications — by its name or its id.",
		Attributes: map[string]schema.Attribute{
			"name": name,
			"id":   id,
		},
	}
}

func (d *namespaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m namespaceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !m.ID.IsNull() {
		n, err := d.client.Cloud.Namespaces.Get(ctx, m.ID.ValueString())
		if err != nil {
			failed(&resp.Diagnostics, "read the namespace", err)
			return
		}
		m.ID, m.Name = types.StringValue(n.ID), types.StringValue(n.Name)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
		return
	}
	all, err := d.client.Cloud.Namespaces.List(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "list the namespaces", err)
		return
	}
	n, ok := byName(all, m.Name.ValueString(), func(n yet.Namespace) string { return n.Name })
	if !ok {
		resp.Diagnostics.AddError("No such namespace", "No namespace named "+m.Name.ValueString()+" that the key can see.")
		return
	}
	m.ID, m.Name = types.StringValue(n.ID), types.StringValue(n.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// ── data "yet_cloud_runtime" ─────────────────────────────────────────────────

type runtimeDataSource struct{ dataBase }

func newRuntimeDataSource() datasource.DataSource { return &runtimeDataSource{} }

func (d *runtimeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_runtime"
}

func (d *runtimeDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	name, id := nameOrID("Its name")
	resp.Schema = schema.Schema{
		MarkdownDescription: "A runtime, by its id, or by its name — and its server's, when another server has one of that name.",
		Attributes: map[string]schema.Attribute{
			"name":  name,
			"id":    id,
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
			"region": computed("A Lambda runtime's region."),
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
	if !m.ID.IsNull() {
		rt, err := d.client.Cloud.Runtimes.Get(ctx, m.ID.ValueString())
		if err != nil {
			failed(&resp.Diagnostics, "read the runtime", err)
			return
		}
		m.from(rt)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
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
	name, id := nameOrID("The domain: `acme.example.org`")
	resp.Schema = schema.Schema{
		MarkdownDescription: "A DNS zone, by its domain or its id.",
		Attributes: map[string]schema.Attribute{
			"name": name,
			"id":   id,
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
	if !m.ID.IsNull() {
		z, ok = byName(all, m.ID.ValueString(), func(z yet.Zone) string { return z.ID })
	}
	if !ok {
		resp.Diagnostics.AddError("No such zone", "No zone for "+cmp.Or(m.Name.ValueString(), m.ID.ValueString())+".")
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

// ── data "yet_cloud_usage" ───────────────────────────────────────────────────

type usageDataSource struct{ dataBase }

func newUsageDataSource() datasource.DataSource { return &usageDataSource{} }

type usageModel struct {
	Accounts []usageAccountModel `tfsdk:"accounts"`
	Limits   []usageLimitModel   `tfsdk:"limits"`
}

type usageAccountModel struct {
	ID      types.String `tfsdk:"id"`
	Name    types.String `tfsdk:"name"`
	Version types.Int64  `tfsdk:"version"`
	Problem types.String `tfsdk:"problem"`
}

type usageLimitModel struct {
	Account types.String `tfsdk:"account"`
	Region  types.String `tfsdk:"region"`
	Service types.String `tfsdk:"service"`
	Name    types.String `tfsdk:"name"`
	Used    types.Int64  `tfsdk:"used"`
	Max     types.Int64  `tfsdk:"max"`
	Unit    types.String `tfsdk:"unit"`
	Problem types.String `tfsdk:"problem"`
}

func (d *usageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_usage"
}

func (d *usageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	number := func(desc string) schema.Int64Attribute {
		return schema.Int64Attribute{MarkdownDescription: desc, Computed: true}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "What Cloud has, and what it uses of each AWS account connected against what the account " +
			"allows: its quotas, in each region Cloud uses there — to see one coming before AWS refuses what Cloud makes.",
		Attributes: map[string]schema.Attribute{
			"accounts": schema.ListNestedAttribute{
				MarkdownDescription: "Each AWS account connected.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":      computed("The account's id in Yet."),
					"name":    computed("Its name in Yet."),
					"version": number("Its stack's template version."),
					"problem": computed("Why its quotas were not read — a stack too old, a role refused — empty when they were."),
				}},
			},
			"limits": schema.ListNestedAttribute{
				MarkdownDescription: "Each quota, and each of Cloud's own counts.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"account": computed("The account's name — empty for Cloud's own counts."),
					"region":  computed("Its region — empty for an account-wide quota."),
					"service": computed("`EC2`, `Lambda`, `CloudFront`, `IAM`, `S3` — `Yet` for Cloud's own."),
					"name":    computed("What it counts."),
					"used":    number("How much is used."),
					"max":     number("What is allowed: 0 unknown, or no limit."),
					"unit":    computed("`bytes` when it counts them, empty otherwise."),
					"problem": computed("Why it was not counted, as AWS said."),
				}},
			},
		},
	}
}

func (d *usageDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	u, err := d.client.Cloud.Usage(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "read the usage", err)
		return
	}
	m := usageModel{Accounts: []usageAccountModel{}, Limits: []usageLimitModel{}}
	add := func(account, region string, l yet.UsageLimit) {
		m.Limits = append(m.Limits, usageLimitModel{
			Account: types.StringValue(account), Region: types.StringValue(region), Service: types.StringValue(l.Service),
			Name: types.StringValue(l.Name), Used: types.Int64Value(l.Used), Max: types.Int64Value(l.Max),
			Unit: types.StringValue(l.Unit), Problem: types.StringValue(l.Problem),
		})
	}
	for _, l := range u.Yet {
		add("", "", l)
	}
	for _, a := range u.Accounts {
		m.Accounts = append(m.Accounts, usageAccountModel{ID: types.StringValue(a.ID), Name: types.StringValue(a.Name),
			Version: types.Int64Value(int64(a.Version)), Problem: types.StringValue(a.Problem)})
		for _, l := range a.Global {
			add(a.Name, "", l)
		}
		for _, r := range a.Regions {
			for _, l := range r.Limits {
				add(a.Name, r.Region, l)
			}
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
