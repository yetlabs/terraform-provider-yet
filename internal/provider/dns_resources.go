package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

// ── yet_cloud_dns_zone ───────────────────────────────────────────────────────

type dnsZoneResource struct{ base }

func newDNSZoneResource() resource.Resource { return &dnsZoneResource{} }

type dnsZoneModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Nameservers types.List   `tfsdk:"nameservers"`
	Status      types.String `tfsdk:"status"`
}

func (r *dnsZoneResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_dns_zone"
}

func (r *dnsZoneResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A domain whose records Cloud keeps.\n\n" +
			"-> **Note:** Point the domain's registrar at `nameservers`: the zone becomes active once they answer for it.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr,
			"name": schema.StringAttribute{
				MarkdownDescription: "The domain: `acme.example.org`.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"nameservers": schema.ListAttribute{
				MarkdownDescription: "Where the domain's registrar has to point.",
				ElementType:         types.StringType,
				Computed:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "`pending` until its nameservers answer for it, `active` after.",
				Computed:            true,
			},
		},
	}
}

func (m *dnsZoneModel) from(ctx context.Context, z *yet.Zone, d *diagsSink) {
	m.ID = types.StringValue(z.ID)
	m.Name = types.StringValue(z.Name)
	m.Nameservers = listOf(ctx, z.Nameservers, &d.d)
	m.Status = types.StringValue(z.Status)
}

func (r *dnsZoneResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsZoneModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	z, err := r.client.Cloud.DNS.CreateZone(ctx, plan.Name.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "create the zone", err)
		return
	}
	d := &diagsSink{}
	plan.from(ctx, z, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *dnsZoneResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsZoneModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	z, _, err := r.client.Cloud.DNS.Zone(ctx, state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the zone", err)
		}
		return
	}
	d := &diagsSink{}
	state.from(ctx, z, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update has nothing to change: a zone is its name.
func (r *dnsZoneResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dnsZoneModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *dnsZoneResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsZoneModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if err := r.client.Cloud.DNS.DeleteZone(ctx, state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the zone", err)
	}
}

func (r *dnsZoneResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importID(ctx, req, resp)
}

// ── yet_cloud_dns_record ─────────────────────────────────────────────────────

type dnsRecordResource struct{ base }

func newDNSRecordResource() resource.Resource { return &dnsRecordResource{} }

type dnsRecordModel struct {
	ID     types.String `tfsdk:"id"`
	ZoneID types.String `tfsdk:"zone_id"`
	Name   types.String `tfsdk:"name"`
	Type   types.String `tfsdk:"type"`
	TTL    types.Int64  `tfsdk:"ttl"`
	Values types.List   `tfsdk:"values"`
	FQDN   types.String `tfsdk:"fqdn"`
}

func (r *dnsRecordResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_dns_record"
}

func (r *dnsRecordResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One name's values of one type in a zone. The records Cloud keeps for environments are its own; these are the company's.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr,
			"zone_id": schema.StringAttribute{
				MarkdownDescription: "The zone it is in.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name before the zone's: `www`; `@` for the zone's root.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "`A`, `AAAA`, `CNAME`, `MX`, `TXT`, `CAA`, `SRV` or `NS`.",
				Required:            true,
				PlanModifiers:       replace,
				Validators: []validator.String{
					stringvalidator.OneOf("A", "AAAA", "CNAME", "MX", "TXT", "CAA", "SRV", "NS"),
				},
			},
			"ttl": schema.Int64Attribute{
				MarkdownDescription: "How long resolvers keep it, in seconds: `300` unless set.",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(300),
				Validators:          []validator.Int64{int64validator.Between(30, 172800)},
			},
			"values": schema.ListAttribute{
				MarkdownDescription: "Its values: addresses, names, or text.",
				ElementType:         types.StringType,
				Required:            true,
				Validators:          []validator.List{listvalidator.SizeAtLeast(1)},
			},
			"fqdn": schema.StringAttribute{
				MarkdownDescription: "Its full name.",
				Computed:            true,
			},
		},
	}
}

func (m *dnsRecordModel) from(ctx context.Context, rec *yet.Record, d *diagsSink) {
	m.ID = types.StringValue(rec.ID)
	m.ZoneID = types.StringValue(rec.ZoneID)
	// The root, kept as "" or "@": as it was said.
	if !(rec.Name == "" && m.Name.ValueString() == "@") {
		m.Name = types.StringValue(rec.Name)
	}
	m.Type = types.StringValue(rec.Type)
	m.TTL = types.Int64Value(int64(rec.TTL))
	// Cloud writes values its own way — no final dot, TXT in quotes. What was
	// said stays in the state while it means the same, so plans stay empty.
	var said []string
	if known(m.Values) {
		d.d.Append(m.Values.ElementsAs(ctx, &said, false)...)
	}
	if !sameValues(rec.Type, said, rec.Values) {
		m.Values = listOf(ctx, rec.Values, &d.d)
	}
	m.FQDN = types.StringValue(rec.FQDN)
}

func (r *dnsRecordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	values := stringsOf(ctx, plan.Values, &d.d)
	if resp.Diagnostics.Append(d.d...); resp.Diagnostics.HasError() {
		return
	}
	rec, err := r.client.Cloud.DNS.CreateRecord(ctx, plan.ZoneID.ValueString(), yet.RecordInput{
		Name: plan.Name.ValueString(), Type: plan.Type.ValueString(), TTL: int(plan.TTL.ValueInt64()), Values: *values,
	})
	if err != nil {
		failed(&resp.Diagnostics, "create the record", err)
		return
	}
	plan.from(ctx, rec, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *dnsRecordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, records, err := r.client.Cloud.DNS.Zone(ctx, state.ZoneID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the record", err)
		}
		return
	}
	for _, rec := range records {
		if rec.ID == state.ID.ValueString() {
			d := &diagsSink{}
			state.from(ctx, &rec, d)
			resp.Diagnostics.Append(d.d...)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *dnsRecordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dnsRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	values := stringsOf(ctx, plan.Values, &d.d)
	if resp.Diagnostics.Append(d.d...); resp.Diagnostics.HasError() {
		return
	}
	rec, err := r.client.Cloud.DNS.UpdateRecord(ctx, plan.ZoneID.ValueString(), plan.ID.ValueString(), int(plan.TTL.ValueInt64()), *values)
	if err != nil {
		failed(&resp.Diagnostics, "change the record", err)
		return
	}
	plan.from(ctx, rec, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *dnsRecordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	err := r.client.Cloud.DNS.DeleteRecord(ctx, state.ZoneID.ValueString(), state.ID.ValueString())
	if err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the record", err)
	}
}

func sameValues(kind string, said, kept []string) bool {
	if len(said) != len(kept) {
		return false
	}
	for i := range said {
		if plain(kind, said[i]) != plain(kind, kept[i]) {
			return false
		}
	}
	return true
}

// plain is a value as Cloud keeps it, written one way.
func plain(kind, v string) string {
	v = strings.TrimSpace(v)
	switch kind {
	case "CNAME", "NS", "MX":
		return strings.TrimSuffix(strings.ToLower(strings.Join(strings.Fields(v), " ")), ".")
	case "TXT":
		// One string, however it was quoted or split in 255s.
		return strings.ReplaceAll(strings.Trim(v, `"`), `" "`, "")
	}
	return v
}

// ImportState takes "<zone_id>/<record_id>".
func (r *dnsRecordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	zone, record, ok := strings.Cut(req.ID, "/")
	if !ok || zone == "" || record == "" {
		resp.Diagnostics.AddError("Unexpected import id", "Import a record as <zone_id>/<record_id>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("zone_id"), zone)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), record)...)
}
