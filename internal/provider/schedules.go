package provider

import (
	"context"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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

// ── yet_cloud_application_schedule ───────────────────────────────────────────

type scheduleResource struct{ base }

func newScheduleResource() resource.Resource { return &scheduleResource{} }

type scheduleModel struct {
	ID            types.String `tfsdk:"id"`
	ApplicationID types.String `tfsdk:"application_id"`
	Name          types.String `tfsdk:"name"`
	Cron          types.String `tfsdk:"cron"`
	TimeZone      types.String `tfsdk:"time_zone"`
	Method        types.String `tfsdk:"method"`
	Path          types.String `tfsdk:"path"`
	Stages        types.Set    `tfsdk:"stages"`
}

func (r *scheduleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_application_schedule"
}

func (r *scheduleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A path of an application's called on a cron, on its stages: on a runtime a CronJob beside it, which asks " +
			"its own Service; on Lambda, EventBridge Scheduler in the account calling its function straight — up to its `timeout_seconds`. " +
			"The application gets a request, `X-Yet-Schedule` the schedule's name and `X-Yet-Schedule-Token` what it is handed as " +
			"`YET_SCHEDULE_TOKEN` — check it: one without it is anyone's. A status from 400 fails the run. One at a time on a runtime: " +
			"a run still going when the next is due, the next waits.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr,
			"application_id": schema.StringAttribute{
				MarkdownDescription: "The application (`yet_cloud_application`'s `id`).",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Its name, the application's only one so named: lowercase letters, digits and dashes, up to 40.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`), "lowercase letters, digits and dashes")},
			},
			"cron": schema.StringAttribute{
				MarkdownDescription: "When: five fields — minute, hour, day of the month, month, weekday — a day of the month or a weekday, " +
					"not both. `*/5 * * * *` is every five minutes; `0 3 * * 1-5`, at three on weekdays.",
				Required: true,
			},
			"time_zone": schema.StringAttribute{
				MarkdownDescription: "The cron's time zone, as the tz database names it — `America/Sao_Paulo`; UTC when left out.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
			},
			"method": schema.StringAttribute{
				MarkdownDescription: "`GET`, `POST`, `PUT`, `PATCH` or `DELETE`; `POST` when left out.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("POST"),
				Validators:          []validator.String{stringvalidator.OneOf("GET", "POST", "PUT", "PATCH", "DELETE")},
			},
			"path": schema.StringAttribute{
				MarkdownDescription: "What it calls, from `/`: `/api/cron/reconcile`, a query too.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^/\S*$`), "a path from /")},
			},
			"stages": schema.SetAttribute{
				MarkdownDescription: "The application's stages it runs on, by environment id; every one when left out.",
				ElementType:         types.StringType,
				Optional:            true,
			},
		},
	}
}

func (m *scheduleModel) input(ctx context.Context, d *diagsSink) yet.ScheduleInput {
	in := yet.ScheduleInput{Name: strPtr(m.Name), Cron: strPtr(m.Cron), TimeZone: strPtr(m.TimeZone), Method: strPtr(m.Method), Path: strPtr(m.Path)}
	stages := []string{}
	if s := stringsOf(ctx, m.Stages, &d.d); s != nil {
		stages = *s
	}
	in.Stages = &stages
	return in
}

func (m *scheduleModel) from(ctx context.Context, s *yet.Schedule, d *diagsSink) {
	m.ID, m.ApplicationID, m.Name = types.StringValue(s.ID), types.StringValue(s.ApplicationID), types.StringValue(s.Name)
	m.Cron, m.TimeZone = types.StringValue(s.Cron), types.StringValue(s.TimeZone)
	m.Method, m.Path = types.StringValue(s.Method), types.StringValue(s.Path)
	if len(s.Stages) > 0 || !m.Stages.IsNull() {
		m.Stages = setOf(ctx, s.Stages, &d.d)
	}
}

func (r *scheduleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan scheduleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	s, err := r.client.Cloud.Schedules.Create(ctx, plan.ApplicationID.ValueString(), plan.input(ctx, d))
	if err != nil {
		failed(&resp.Diagnostics, "create the schedule", err)
		return
	}
	plan.from(ctx, s, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *scheduleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state scheduleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.Cloud.Schedules.Get(ctx, state.ApplicationID.ValueString(), state.ID.ValueString())
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the schedule", err)
		}
		return
	}
	d := &diagsSink{}
	state.from(ctx, s, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *scheduleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan scheduleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := &diagsSink{}
	s, err := r.client.Cloud.Schedules.Update(ctx, plan.ApplicationID.ValueString(), plan.ID.ValueString(), plan.input(ctx, d))
	if err != nil {
		failed(&resp.Diagnostics, "change the schedule", err)
		return
	}
	plan.from(ctx, s, d)
	resp.Diagnostics.Append(d.d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *scheduleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state scheduleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if err := r.client.Cloud.Schedules.Delete(ctx, state.ApplicationID.ValueString(), state.ID.ValueString()); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "delete the schedule", err)
	}
}

// ImportState takes "<application_id>/<schedule_id>".
func (r *scheduleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	app, id, ok := strings.Cut(req.ID, "/")
	if !ok || app == "" || id == "" {
		resp.Diagnostics.AddError("Unexpected import id", "Import a schedule as <application_id>/<schedule_id>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("application_id"), app)...)
}
