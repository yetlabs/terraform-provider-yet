package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	yet "github.com/yetlabs/yet-sdk-go"
)

/*
An application's ports and its checks, as yet_cloud_application has them.
Of its public ports one is the default — the first, unless one says so —
answering at each stage's address; the others answer at their own, the
port's name before it, or the address one says. Terraform says at plan which
one that is, and what `port` becomes, as yet will: else every apply would
find them changed. An address yet would clear — on a private port or the
default — it refuses instead, so what is said stays what is kept.
*/

var (
	portAttrTypes = map[string]attr.Type{
		"name": types.StringType, "port": types.Int64Type, "protocol": types.StringType,
		"public": types.BoolType, "yap": types.BoolType, "default": types.BoolType, "address": types.StringType,
	}
	probeAttrTypes = map[string]attr.Type{
		"port": types.StringType, "protocol": types.StringType, "path": types.StringType,
		"period_seconds": types.Int64Type, "timeout_seconds": types.Int64Type, "failure_threshold": types.Int64Type,
	}
	objectAs   = basetypes.ObjectAsOptions{}
	portNameRe = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,13}[a-z0-9])?$`)
	labelRe    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	protocol   = stringvalidator.OneOf("http", "grpc", "tcp")
)

type appPortModel struct {
	Name     types.String `tfsdk:"name"`
	Port     types.Int64  `tfsdk:"port"`
	Protocol types.String `tfsdk:"protocol"`
	Public   types.Bool   `tfsdk:"public"`
	YaP      types.Bool   `tfsdk:"yap"`
	Default  types.Bool   `tfsdk:"default"`
	Address  types.String `tfsdk:"address"`
}

type probeModel struct {
	Port             types.String `tfsdk:"port"`
	Protocol         types.String `tfsdk:"protocol"`
	Path             types.String `tfsdk:"path"`
	PeriodSeconds    types.Int64  `tfsdk:"period_seconds"`
	TimeoutSeconds   types.Int64  `tfsdk:"timeout_seconds"`
	FailureThreshold types.Int64  `tfsdk:"failure_threshold"`
}

func portsAttr() schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		MarkdownDescription: "Every port it listens on — `1` to `10`, names and numbers each once — replacing what it had. " +
			"Left out, one `http` port on `port`, public and the default.",
		Optional:      true,
		Computed:      true,
		Validators:    []validator.List{listvalidator.SizeBetween(1, 10)},
		PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
		NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Its name: `1` to `15` lowercase letters, digits and dashes, a letter first.",
				Required:            true,
				Validators: []validator.String{stringvalidator.RegexMatches(portNameRe,
					"must be 1 to 15 lowercase letters, digits and dashes, a letter first")},
			},
			"port": schema.Int64Attribute{
				MarkdownDescription: "The number it listens on, `1` to `65535`.",
				Required:            true,
				Validators:          []validator.Int64{int64validator.Between(1, 65535)},
			},
			"protocol": schema.StringAttribute{
				MarkdownDescription: "`http` (the default), `grpc` or `tcp`.",
				Optional:            true, Computed: true,
				Default:    stringdefault.StaticString("http"),
				Validators: []validator.String{protocol},
			},
			"public": schema.BoolAttribute{
				MarkdownDescription: "Whether it answers on the internet too, not only inside the cluster; a `tcp` one cannot, for now.",
				Optional:            true, Computed: true,
				Default: booldefault.StaticBool(false),
			},
			"yap": schema.BoolAttribute{
				MarkdownDescription: "YaP — Yet-Aware Proxy: for a public `http` port, only the company's people get through, signed in " +
					"with Yet; what it answers is told who they are — `X-Yet-Email`, `X-Yet-Name`, `X-Yet-Groups` and " +
					"`X-Yet-Assertion`, the ID token Yet signed. Who gets in is the application's `yap_everyone` and `yap_allowed`.",
				Optional: true, Computed: true,
				Default: booldefault.StaticBool(false),
			},
			"default": schema.BoolAttribute{
				MarkdownDescription: "Whether it is the public one each stage's address answers on — the first public, unless one says so. " +
					"Another public one answers at an address of its own, its name before the stage's: `admin-checkout.acme.example.com`, " +
					"or `admin.acme.example.com` at a zone's `www`.",
				Optional: true, Computed: true,
			},
			"address": schema.StringAttribute{
				MarkdownDescription: "For a public port that is not the default, the subdomain it answers at instead of its name " +
					"before the stage's address: `back-office` answers at `back-office.staging.acme.example.com`. A lowercase label, " +
					"its own among the ports, and on each stage not another application's.",
				Optional: true,
				Validators: []validator.String{stringvalidator.RegexMatches(labelRe,
					"must be a subdomain label: 1 to 63 lowercase letters, digits and dashes, neither end a dash")},
			},
		}},
	}
}

func probeAttr(desc string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: desc + " Left out, none: Kubernetes's own.",
		Optional:            true,
		Attributes: map[string]schema.Attribute{
			"port": schema.StringAttribute{
				MarkdownDescription: "The name of the port it checks, one of `ports`.",
				Required:            true,
			},
			"protocol": schema.StringAttribute{
				MarkdownDescription: "`http` (the default), `grpc` or `tcp`.",
				Optional:            true, Computed: true,
				Default:    stringdefault.StaticString("http"),
				Validators: []validator.String{protocol},
			},
			"path": schema.StringAttribute{
				MarkdownDescription: "What an `http` check asks for, from `/`.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^/`), "must start with /")},
			},
			"period_seconds": schema.Int64Attribute{
				MarkdownDescription: "How often it checks, `1` to `300` seconds: `10` unless set.",
				Optional:            true, Computed: true,
				Default:    int64default.StaticInt64(10),
				Validators: []validator.Int64{int64validator.Between(1, 300)},
			},
			"timeout_seconds": schema.Int64Attribute{
				MarkdownDescription: "How long it waits, `1` to `60` seconds and no longer than `period_seconds`: `1` unless set.",
				Optional:            true, Computed: true,
				Default:    int64default.StaticInt64(1),
				Validators: []validator.Int64{int64validator.Between(1, 60)},
			},
			"failure_threshold": schema.Int64Attribute{
				MarkdownDescription: "How many failures in a row count, `1` to `30`: `3` unless set.",
				Optional:            true, Computed: true,
				Default:    int64default.StaticInt64(3),
				Validators: []validator.Int64{int64validator.Between(1, 30)},
			},
		},
	}
}

func portsIn(ctx context.Context, v types.List, d *diagsSink) []appPortModel {
	var out []appPortModel
	if known(v) {
		d.d.Append(v.ElementsAs(ctx, &out, false)...)
	}
	return out
}

func portsOf(ctx context.Context, ps []yet.AppPort, d *diagsSink) types.List {
	models := make([]appPortModel, len(ps))
	for i, p := range ps {
		models[i] = appPortModel{
			Name: types.StringValue(p.Name), Port: types.Int64Value(int64(p.Port)), Protocol: types.StringValue(p.Protocol),
			Public: types.BoolValue(p.Public), YaP: types.BoolValue(p.YaP), Default: types.BoolValue(p.Default), Address: types.StringNull(),
		}
		if p.Address != "" {
			models[i].Address = types.StringValue(p.Address)
		}
	}
	v, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: portAttrTypes}, models)
	d.d.Append(diags...)
	return v
}

func probeIn(ctx context.Context, v types.Object, d *diagsSink) *yet.Probe {
	if !known(v) {
		return nil
	}
	var m probeModel
	d.d.Append(v.As(ctx, &m, objectAs)...)
	return &yet.Probe{
		Port: m.Port.ValueString(), Protocol: m.Protocol.ValueString(), Path: m.Path.ValueString(),
		PeriodSeconds: int(m.PeriodSeconds.ValueInt64()), TimeoutSeconds: int(m.TimeoutSeconds.ValueInt64()),
		FailureThreshold: int(m.FailureThreshold.ValueInt64()),
	}
}

func probeOf(ctx context.Context, p *yet.Probe, d *diagsSink) types.Object {
	if p == nil {
		return types.ObjectNull(probeAttrTypes)
	}
	m := probeModel{
		Port: types.StringValue(p.Port), Protocol: types.StringValue(p.Protocol), Path: types.StringNull(),
		PeriodSeconds: types.Int64Value(int64(p.PeriodSeconds)), TimeoutSeconds: types.Int64Value(int64(p.TimeoutSeconds)),
		FailureThreshold: types.Int64Value(int64(p.FailureThreshold)),
	}
	if p.Path != "" {
		m.Path = types.StringValue(p.Path)
	}
	v, diags := types.ObjectValueFrom(ctx, probeAttrTypes, m)
	d.d.Append(diags...)
	return v
}

// ValidateConfig refuses, at plan, ports and checks yet would: what each
// field's own bounds cannot say.
func (r *applicationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config applicationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	ports := portsIn(ctx, config.Ports, d)
	resp.Diagnostics.Append(d.d...)
	names, numbers, addresses, defaults := map[string]bool{}, map[int64]bool{}, map[string]bool{}, 0
	for i, p := range ports {
		at := path.Root("ports").AtListIndex(i)
		if known(p.Name) && names[p.Name.ValueString()] {
			resp.Diagnostics.AddAttributeError(at.AtName("name"), "Two ports are named so", "Each port's name is its own.")
		}
		if known(p.Port) && numbers[p.Port.ValueInt64()] {
			resp.Diagnostics.AddAttributeError(at.AtName("port"), "Two ports listen on it", "Each port's number is its own.")
		}
		names[p.Name.ValueString()], numbers[p.Port.ValueInt64()] = true, true
		if p.Protocol.ValueString() == "tcp" && p.Public.ValueBool() {
			resp.Diagnostics.AddAttributeError(at.AtName("public"), "A TCP port is not public", "A tcp port is inside the cluster only, for now.")
		}
		if p.YaP.ValueBool() && !p.Public.IsUnknown() && !p.Public.ValueBool() {
			resp.Diagnostics.AddAttributeError(at.AtName("yap"), "A YaP port is public", "Only a public port lets the company's people through, signed in.")
		}
		if p.YaP.ValueBool() && known(p.Protocol) && p.Protocol.ValueString() != "http" {
			resp.Diagnostics.AddAttributeError(at.AtName("yap"), "A YaP port is an HTTP one", "A browser signs in to it: grpc and tcp ones cannot be YaP.")
		}
		if p.Default.ValueBool() {
			defaults++
			if !p.Public.IsUnknown() && !p.Public.ValueBool() {
				resp.Diagnostics.AddAttributeError(at.AtName("default"), "The default port is public", "Only a public port can be the default.")
			}
		}
		if known(p.Address) {
			if addresses[p.Address.ValueString()] {
				resp.Diagnostics.AddAttributeError(at.AtName("address"), "Two ports answer there", "Each port's address is its own.")
			}
			addresses[p.Address.ValueString()] = true
			if !p.Public.IsUnknown() && !p.Public.ValueBool() {
				resp.Diagnostics.AddAttributeError(at.AtName("address"), "Only a public port has an address", "A private port answers inside the cluster only.")
			}
		}
	}
	if defaults > 1 {
		resp.Diagnostics.AddAttributeError(path.Root("ports"), "One default port", "At most one port is the default.")
	}
	// The default answers at the stage's own address: the one said, or the
	// first public when none is.
	if i := defaultPort(ports); i >= 0 && !ports[i].Address.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("ports").AtListIndex(i).AtName("address"), "The default port has no address of its own",
			"The default answers at each stage's address; an address is for another public port.")
	}
	for _, x := range []struct {
		name  string
		probe types.Object
	}{{"readiness", config.Readiness}, {"liveness", config.Liveness}} {
		if !known(x.probe) {
			continue
		}
		var p probeModel
		resp.Diagnostics.Append(x.probe.As(ctx, &p, objectAs)...)
		at := path.Root(x.name)
		if known(p.Port) && known(config.Ports) && !names[p.Port.ValueString()] {
			resp.Diagnostics.AddAttributeError(at.AtName("port"), "No such port", "A check is on one of its ports, by name.")
		}
		isHTTP := p.Protocol.IsNull() || p.Protocol.ValueString() == "http"
		switch {
		case p.Protocol.IsUnknown() || p.Path.IsUnknown():
		case isHTTP && p.Path.IsNull():
			resp.Diagnostics.AddAttributeError(at.AtName("path"), "An HTTP check has a path", "Say the path it asks for, from /.")
		case !isHTTP && !p.Path.IsNull():
			resp.Diagnostics.AddAttributeError(at.AtName("path"), "Only an HTTP check has a path", "A grpc or tcp check has none.")
		}
		period, timeout := int64(10), int64(1)
		if known(p.PeriodSeconds) {
			period = p.PeriodSeconds.ValueInt64()
		}
		if known(p.TimeoutSeconds) {
			timeout = p.TimeoutSeconds.ValueInt64()
		}
		if timeout > period {
			resp.Diagnostics.AddAttributeError(at.AtName("timeout_seconds"), "It waits too long", "A check waits no longer than between checks.")
		}
	}
}

// defaultPort is which of the ports said is the default, as yet picks it;
// -1 when there is none, or it cannot be known yet.
func defaultPort(ports []appPortModel) int {
	for i, p := range ports {
		if p.Default.IsUnknown() {
			return -1
		}
		if p.Default.ValueBool() {
			return i
		}
	}
	for i, p := range ports {
		if p.Public.IsUnknown() {
			return -1
		}
		if p.Public.ValueBool() {
			return i
		}
	}
	return -1
}

// ModifyPlan says which port is the default, and `port` as its number, when
// ports are said; when only `port` is, it moves the default's number.
func (r *applicationResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan, config applicationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	ports := portsIn(ctx, plan.Ports, d)
	if len(ports) == 0 {
		return
	}
	if config.Ports.IsNull() {
		if !known(plan.Port) {
			return
		}
		for i := range ports {
			if ports[i].Default.ValueBool() {
				ports[i].Port = plan.Port
			}
		}
	} else {
		said := portsIn(ctx, config.Ports, d)
		if len(said) != len(ports) {
			return
		}
		chosen := -1
		for i, p := range said {
			if !known(ports[i].Public) || p.Default.IsUnknown() {
				return
			}
			if p.Default.ValueBool() {
				chosen = i
			}
		}
		for i := range ports {
			if chosen < 0 && ports[i].Public.ValueBool() {
				chosen = i
			}
		}
		for i := range ports {
			ports[i].Default = types.BoolValue(i == chosen)
		}
		if chosen < 0 {
			chosen = 0
		}
		plan.Port = ports[chosen].Port
	}
	v, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: portAttrTypes}, ports)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(d.d...)
	plan.Ports = v
	resp.Diagnostics.Append(resp.Plan.Set(ctx, plan)...)
}
