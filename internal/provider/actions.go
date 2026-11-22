package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

/*
Actions: what yet does once, when asked — a backup, a release, a promotion —
rather than something it keeps. Terraform 1.14 and later run them, by hand
(`terraform apply -invoke`) or on a resource's lifecycle events.
*/

type actionBase struct{ base }

func (a *actionBase) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	a.configure(req.ProviderData, &resp.Diagnostics)
}

// ready is whether the provider handed the action its client; Terraform asks
// an action's schema before configuring the provider.
func (a *actionBase) ready(resp *action.InvokeResponse) bool {
	if a.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "The yet provider has no client for this action.")
		return false
	}
	return true
}

// waitFor is how long an action waits, in minutes: what was said, or the default.
func waitFor(v types.Int64, minutes int64) time.Duration {
	if known(v) {
		minutes = v.ValueInt64()
	}
	return time.Duration(minutes) * time.Minute
}

// ── yet_cloud_database_adopt ─────────────────────────────────────────────────

type databaseAdoptAction struct{ actionBase }

func newDatabaseAdoptAction() action.Action { return &databaseAdoptAction{} }

type databaseAdoptModel struct {
	DatabaseID    types.String `tfsdk:"database_id"`
	ApplicationID types.String `tfsdk:"application_id"`
}

func (a *databaseAdoptAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_database_adopt"
}

func (a *databaseAdoptAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Makes an add-on kept from an application gone another application's own, on a stage it runs on: " +
			"it stays where it runs, its data with it, and the application is handed it in its variable.",
		Attributes: map[string]schema.Attribute{
			"database_id": schema.StringAttribute{
				MarkdownDescription: "The add-on kept, with no application.",
				Required:            true,
			},
			"application_id": schema.StringAttribute{
				MarkdownDescription: "The application that adopts it.",
				Required:            true,
			},
		},
	}
}

func (a *databaseAdoptAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var m databaseAdoptModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !a.ready(resp) {
		return
	}
	db, err := a.client.Cloud.Databases.Adopt(ctx, m.DatabaseID.ValueString(), m.ApplicationID.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "adopt the add-on", err)
		return
	}
	resp.SendProgress(action.InvokeProgressEvent{Message: "Adopted, in " + db.EnvVar})
}

// ── yet_cloud_database_backup ────────────────────────────────────────────────

type databaseBackupAction struct{ actionBase }

func newDatabaseBackupAction() action.Action { return &databaseBackupAction{} }

type databaseBackupModel struct {
	DatabaseID types.String `tfsdk:"database_id"`
	Wait       types.Bool   `tfsdk:"wait"`
	Timeout    types.Int64  `tfsdk:"timeout"`
}

func (a *databaseBackupAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_database_backup"
}

func (a *databaseBackupAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Backs up a database now — one copy per stage it runs on — and waits for the copies to be taken.\n\n" +
			"-> **Note:** Copies are kept for an Overlite on a runtime of the company's. Its runtime's agent takes them at its next look.",
		Attributes: map[string]schema.Attribute{
			"database_id": schema.StringAttribute{
				MarkdownDescription: "The database to back up.",
				Required:            true,
			},
			"wait": schema.BoolAttribute{
				MarkdownDescription: "Whether to wait for the copies to be taken: `true` unless set.",
				Optional:            true,
			},
			"timeout": schema.Int64Attribute{
				MarkdownDescription: "Up to how many minutes to wait: `10` unless set.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.Between(1, 120)},
			},
		},
	}
}

func (a *databaseBackupAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var m databaseBackupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !a.ready(resp) {
		return
	}
	id := m.DatabaseID.ValueString()
	made, err := a.client.Cloud.Databases.BackUp(ctx, id)
	if err != nil {
		failed(&resp.Diagnostics, "back the database up", err)
		return
	}
	resp.SendProgress(action.InvokeProgressEvent{Message: fmt.Sprintf("%d backup(s) asked for", len(made))})
	if known(m.Wait) && !m.Wait.ValueBool() {
		return
	}
	asked := map[string]bool{}
	for _, b := range made {
		asked[b.ID] = true
	}
	deadline := time.Now().Add(waitFor(m.Timeout, 10))
	for {
		got, err := a.client.Cloud.Databases.Backups(ctx, id)
		if err != nil {
			failed(&resp.Diagnostics, "read the database's backups", err)
			return
		}
		pending := 0
		for _, b := range got.Backups {
			if !asked[b.ID] {
				continue
			}
			switch b.Status {
			case "failed":
				resp.Diagnostics.AddError("A backup failed", b.Problem)
				return
			case "done":
			default:
				pending++
			}
		}
		if pending == 0 {
			resp.SendProgress(action.InvokeProgressEvent{Message: "Backed up"})
			return
		}
		if time.Now().After(deadline) {
			resp.Diagnostics.AddError("Backup still going", "The copies were not taken in time; they go on, and show in Yet's apps.")
			return
		}
		resp.SendProgress(action.InvokeProgressEvent{Message: fmt.Sprintf("%d backup(s) on their way…", pending)})
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// ── yet_cloud_database_restore ───────────────────────────────────────────────

type databaseRestoreAction struct{ actionBase }

func newDatabaseRestoreAction() action.Action { return &databaseRestoreAction{} }

type databaseRestoreModel struct {
	DatabaseID    types.String `tfsdk:"database_id"`
	BackupID      types.String `tfsdk:"backup_id"`
	At            types.String `tfsdk:"at"`
	EnvironmentID types.String `tfsdk:"environment_id"`
	Wait          types.Bool   `tfsdk:"wait"`
	Timeout       types.Int64  `tfsdk:"timeout"`
}

func (a *databaseRestoreAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_database_restore"
}

func (a *databaseRestoreAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Restores one of a database's copies in its place, on the copy's stage — or a BucketDB's version, " +
			"from its history (`at`) — and waits for it to be back.\n\n" +
			"~> **Warning:** The database stops for a moment and starts again on the copy: what was written since is not in it. " +
			"The file before is kept on its volume, `app.db.before-restore`, until the next restore. A BucketDB's version becomes its " +
			"current one, its applications moving to it at their next transaction; what was written since stays in its history.",
		Attributes: map[string]schema.Attribute{
			"database_id": schema.StringAttribute{
				MarkdownDescription: "The database to restore.",
				Required:            true,
			},
			"backup_id": schema.StringAttribute{
				MarkdownDescription: "The copy: one of its backups, or a file uploaded as one. Not with `at`.",
				Optional:            true,
			},
			"at": schema.StringAttribute{
				MarkdownDescription: "A BucketDB's version, by its id in its history (`yet.Databases.Versions`). Not with `backup_id`.",
				Optional:            true,
			},
			"environment_id": schema.StringAttribute{
				MarkdownDescription: "The stage `at` is put back on, for an application's own BucketDB on several: its last unless set.",
				Optional:            true,
			},
			"wait": schema.BoolAttribute{
				MarkdownDescription: "Whether to wait for it to be back: `true` unless set.",
				Optional:            true,
			},
			"timeout": schema.Int64Attribute{
				MarkdownDescription: "Up to how many minutes to wait: `10` unless set.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.Between(1, 120)},
			},
		},
	}
}

func (a *databaseRestoreAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var m databaseRestoreModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !a.ready(resp) {
		return
	}
	id := m.DatabaseID.ValueString()
	if m.BackupID.ValueString() == "" == (m.At.ValueString() == "") {
		resp.Diagnostics.AddError("A copy, or a version", "Set `backup_id` — or, a BucketDB's version, `at` — one of them.")
		return
	}
	var rs *yet.Restore
	var err error
	if at := m.At.ValueString(); at != "" {
		rs, err = a.client.Cloud.Databases.RestoreVersion(ctx, id, m.EnvironmentID.ValueString(), at)
	} else {
		rs, err = a.client.Cloud.Databases.Restore(ctx, id, m.BackupID.ValueString())
	}
	if err != nil {
		failed(&resp.Diagnostics, "restore the database", err)
		return
	}
	resp.SendProgress(action.InvokeProgressEvent{Message: "Restore asked for"})
	if known(m.Wait) && !m.Wait.ValueBool() {
		return
	}
	deadline := time.Now().Add(waitFor(m.Timeout, 10))
	for {
		got, err := a.client.Cloud.Databases.Backups(ctx, id)
		if err != nil {
			failed(&resp.Diagnostics, "read the database's restores", err)
			return
		}
		for _, r := range got.Restores {
			if r.ID != rs.ID {
				continue
			}
			switch r.Status {
			case "done":
				resp.SendProgress(action.InvokeProgressEvent{Message: "Restored"})
				return
			case "failed":
				resp.Diagnostics.AddError("The restore failed", r.Problem)
				return
			}
			resp.SendProgress(action.InvokeProgressEvent{Message: "Restore " + r.Status + "…"})
		}
		if time.Now().After(deadline) {
			resp.Diagnostics.AddError("Restore still going", "It was not done in time; it goes on, and shows in Yet's apps.")
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// ── yet_cloud_application_release ────────────────────────────────────────────

type applicationReleaseAction struct{ actionBase }

func newApplicationReleaseAction() action.Action { return &applicationReleaseAction{} }

type applicationReleaseModel struct {
	ApplicationID types.String `tfsdk:"application_id"`
	Image         types.String `tfsdk:"image"`
}

func (a *applicationReleaseAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_application_release"
}

func (a *applicationReleaseAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Releases an image of an application: a numbered release, deployed to its first stage. " +
			"From there it goes on as the application says — by hand, or promoting by itself.",
		Attributes: map[string]schema.Attribute{
			"application_id": schema.StringAttribute{
				MarkdownDescription: "The application.",
				Required:            true,
			},
			"image": schema.StringAttribute{
				MarkdownDescription: "The image to release: `ghcr.io/acme/api:1.4.0`.",
				Required:            true,
			},
		},
	}
}

func (a *applicationReleaseAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var m applicationReleaseModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !a.ready(resp) {
		return
	}
	rel, dep, err := a.client.Cloud.Applications.Release(ctx, m.ApplicationID.ValueString(), m.Image.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "release the image", err)
		return
	}
	resp.SendProgress(action.InvokeProgressEvent{
		Message: fmt.Sprintf("Release #%d of %s: deploying to its first stage (%s)", rel.Number, rel.Image, dep.Status),
	})
}

// ── yet_cloud_application_promote ────────────────────────────────────────────

type applicationPromoteAction struct{ actionBase }

func newApplicationPromoteAction() action.Action { return &applicationPromoteAction{} }

type applicationPromoteModel struct {
	ApplicationID types.String `tfsdk:"application_id"`
	From          types.String `tfsdk:"from_environment_id"`
}

func (a *applicationPromoteAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_application_promote"
}

func (a *applicationPromoteAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Promotes an application: what runs in one stage goes to the next — staging to production.",
		Attributes: map[string]schema.Attribute{
			"application_id": schema.StringAttribute{
				MarkdownDescription: "The application.",
				Required:            true,
			},
			"from_environment_id": schema.StringAttribute{
				MarkdownDescription: "The stage it goes from; what runs there goes to the stage after it.",
				Required:            true,
			},
		},
	}
}

func (a *applicationPromoteAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var m applicationPromoteModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !a.ready(resp) {
		return
	}
	dep, err := a.client.Cloud.Applications.Promote(ctx, m.ApplicationID.ValueString(), m.From.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "promote the application", err)
		return
	}
	resp.SendProgress(action.InvokeProgressEvent{Message: "Promoted: deploying (" + dep.Status + ")"})
}

// ── yet_cloud_application_build ──────────────────────────────────────────────

type applicationBuildAction struct{ actionBase }

func newApplicationBuildAction() action.Action { return &applicationBuildAction{} }

type applicationBuildModel struct {
	ApplicationID types.String `tfsdk:"application_id"`
	Wait          types.Bool   `tfsdk:"wait"`
	Timeout       types.Int64  `tfsdk:"timeout"`
}

func (a *applicationBuildAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_application_build"
}

func (a *applicationBuildAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Builds an application's branch as it is now — its latest commit — without waiting for a push: " +
			"a release, deployed to its first stage once its runtime has built the image.\n\n" +
			"-> **Note:** The application is built from a repository on the company's GitHub, through yet's GitHub App; " +
			"building deploys to its first stage, so it takes a developer's role there.",
		Attributes: map[string]schema.Attribute{
			"application_id": schema.StringAttribute{
				MarkdownDescription: "The application.",
				Required:            true,
			},
			"wait": schema.BoolAttribute{
				MarkdownDescription: "Whether to wait for the image to be built: `true` unless set.",
				Optional:            true,
			},
			"timeout": schema.Int64Attribute{
				MarkdownDescription: "Up to how many minutes to wait: `20` unless set.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.Between(1, 120)},
			},
		},
	}
}

func (a *applicationBuildAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var m applicationBuildModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !a.ready(resp) {
		return
	}
	id := m.ApplicationID.ValueString()
	b, err := a.client.Cloud.Applications.BuildNow(ctx, id)
	if err != nil {
		failed(&resp.Diagnostics, "build the application", err)
		return
	}
	resp.SendProgress(action.InvokeProgressEvent{Message: fmt.Sprintf("Building %s@%.7s: %s", b.Branch, b.Commit, b.Status)})
	if known(m.Wait) && !m.Wait.ValueBool() {
		return
	}
	deadline := time.Now().Add(waitFor(m.Timeout, 20))
	for {
		switch b.Status {
		case "built":
			resp.SendProgress(action.InvokeProgressEvent{Message: "Built: deploying to its first stage"})
			return
		case "failed":
			resp.Diagnostics.AddError("The build failed", b.Problem)
			return
		}
		if time.Now().After(deadline) {
			resp.Diagnostics.AddError("Build still going", "The image was not built in time; it goes on, and shows in Yet's apps.")
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
		if b, _, err = a.client.Cloud.Applications.Build(ctx, id, b.ID); err != nil {
			failed(&resp.Diagnostics, "read the build", err)
			return
		}
		resp.SendProgress(action.InvokeProgressEvent{Message: "Build " + b.Status + "…"})
	}
}

// ── yet_cloud_application_pod_delete ─────────────────────────────────────────

type applicationPodDeleteAction struct{ actionBase }

func newApplicationPodDeleteAction() action.Action { return &applicationPodDeleteAction{} }

type applicationPodDeleteModel struct {
	ApplicationID types.String `tfsdk:"application_id"`
	Pod           types.String `tfsdk:"pod"`
}

func (a *applicationPodDeleteAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_application_pod_delete"
}

func (a *applicationPodDeleteAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Deletes one of an application's pods, on whichever runtime runs it: its deployment starts a fresh copy " +
			"in its place — a restart of that one copy, the rest left running.\n\n" +
			"-> **Note:** It takes a developer's role in the pod's environment. The runtime's agent deletes the pod at its next look.",
		Attributes: map[string]schema.Attribute{
			"application_id": schema.StringAttribute{
				MarkdownDescription: "The application.",
				Required:            true,
			},
			"pod": schema.StringAttribute{
				MarkdownDescription: "The pod's name, as Yet's apps show it: `api-7d9f8-x2k4p`.",
				Required:            true,
			},
		},
	}
}

func (a *applicationPodDeleteAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var m applicationPodDeleteModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !a.ready(resp) {
		return
	}
	gone, err := a.client.Cloud.Applications.DeletePod(ctx, m.ApplicationID.ValueString(), m.Pod.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "delete the pod", err)
		return
	}
	resp.SendProgress(action.InvokeProgressEvent{Message: gone.Pod + " is going; a fresh copy takes its place"})
}

// ── yet_cloud_dns_zone_verify ────────────────────────────────────────────────

type dnsZoneVerifyAction struct{ actionBase }

func newDNSZoneVerifyAction() action.Action { return &dnsZoneVerifyAction{} }

type dnsZoneVerifyModel struct {
	ZoneID types.String `tfsdk:"zone_id"`
}

func (a *dnsZoneVerifyAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_dns_zone_verify"
}

func (a *dnsZoneVerifyAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Asks a zone's domain for its nameservers again, now, rather than at Cloud's next look: " +
			"right after its registrar was pointed at the zone's `nameservers`.",
		Attributes: map[string]schema.Attribute{
			"zone_id": schema.StringAttribute{
				MarkdownDescription: "The zone.",
				Required:            true,
			},
		},
	}
}

func (a *dnsZoneVerifyAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var m dnsZoneVerifyModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !a.ready(resp) {
		return
	}
	z, err := a.client.Cloud.DNS.VerifyZone(ctx, m.ZoneID.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "verify the zone", err)
		return
	}
	resp.SendProgress(action.InvokeProgressEvent{Message: z.Name + " is " + z.Status})
}

// ── yet_cloud_server_update_agent ────────────────────────────────────────────

type serverUpdateAgentAction struct{ actionBase }

func newServerUpdateAgentAction() action.Action { return &serverUpdateAgentAction{} }

type serverUpdateAgentModel struct {
	ServerID types.String `tfsdk:"server_id"`
}

func (a *serverUpdateAgentAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloud_server_update_agent"
}

func (a *serverUpdateAgentAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Asks a server's agent to update now, to Cloud's version, rather than at its own next try, " +
			"half an hour after the one before: it does at its next check-ins. Only an agent behind Cloud is asked.",
		Attributes: map[string]schema.Attribute{
			"server_id": schema.StringAttribute{
				MarkdownDescription: "The server.",
				Required:            true,
			},
		},
	}
}

func (a *serverUpdateAgentAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var m serverUpdateAgentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !a.ready(resp) {
		return
	}
	sv, err := a.client.Cloud.Servers.UpdateAgent(ctx, m.ServerID.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "ask its agent to update", err)
		return
	}
	resp.SendProgress(action.InvokeProgressEvent{Message: sv.Name + "'s agent asked to update, from " + sv.AgentVersion})
}

// ── yet_settings_mail_send_email ─────────────────────────────────────────────

type sendEmailAction struct{ actionBase }

func newSendEmailAction() action.Action { return &sendEmailAction{} }

type sendEmailModel struct {
	From           types.String      `tfsdk:"from"`
	To             types.List        `tfsdk:"to"`
	Cc             types.List        `tfsdk:"cc"`
	Bcc            types.List        `tfsdk:"bcc"`
	ReplyTo        types.List        `tfsdk:"reply_to"`
	Subject        types.String      `tfsdk:"subject"`
	HTML           types.String      `tfsdk:"html"`
	Text           types.String      `tfsdk:"text"`
	Headers        types.Map         `tfsdk:"headers"`
	Tags           types.Map         `tfsdk:"tags"`
	Attachments    []attachmentModel `tfsdk:"attachments"`
	IdempotencyKey types.String      `tfsdk:"idempotency_key"`
}

type attachmentModel struct {
	Filename      types.String `tfsdk:"filename"`
	ContentBase64 types.String `tfsdk:"content_base64"`
	ContentType   types.String `tfsdk:"content_type"`
}

func (a *sendEmailAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_mail_send_email"
}

func (a *sendEmailAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	addresses := func(what string) schema.ListAttribute {
		return schema.ListAttribute{MarkdownDescription: what, ElementType: types.StringType, Optional: true}
	}
	body := stringvalidator.AtLeastOneOf(path.MatchRoot("html"), path.MatchRoot("text"))
	resp.Schema = schema.Schema{
		MarkdownDescription: "Sends an email from one of the company's subdomains' addresses (`yet_settings_mail_subdomain`), " +
			"through the SES that subdomain sends through: a notice when something is made, say.\n\n" +
			"-> **Note:** Each run sends again. With `idempotency_key`, a run with a key already used answers the email " +
			"it sent then, and sends nothing.",
		Attributes: map[string]schema.Attribute{
			"from": schema.StringAttribute{
				MarkdownDescription: "The sender, at a subdomain: `Acme <avisos@avisos.acme.com>` or the bare address.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(3, 320)},
			},
			"to": schema.ListAttribute{
				MarkdownDescription: "Who it goes to; with `cc` and `bcc`, up to 50.",
				ElementType:         types.StringType,
				Required:            true,
				Validators:          []validator.List{listvalidator.SizeAtLeast(1)},
			},
			"cc":       addresses("Who receives a copy."),
			"bcc":      addresses("Who receives a copy, unseen by the rest."),
			"reply_to": addresses("Where replies go, when not to `from`."),
			"subject": schema.StringAttribute{
				MarkdownDescription: "The subject.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 998)},
			},
			"html": schema.StringAttribute{
				MarkdownDescription: "The body as HTML; `html`, `text` or both.",
				Optional:            true,
				Validators:          []validator.String{body},
			},
			"text": schema.StringAttribute{
				MarkdownDescription: "The body as plain text.",
				Optional:            true,
				Validators:          []validator.String{body},
			},
			"headers": schema.MapAttribute{
				MarkdownDescription: "The message's own headers, by name, besides those the fields above write.",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"tags": schema.MapAttribute{
				MarkdownDescription: "SES's tags, by name, kept with its events: up to 10, letters, digits, `_` and `-`.",
				ElementType:         types.StringType,
				Optional:            true,
				Validators:          []validator.Map{mapvalidator.SizeAtMost(10)},
			},
			"attachments": schema.ListNestedAttribute{
				MarkdownDescription: "Files it carries, up to 10 MB in all.",
				Optional:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"filename": schema.StringAttribute{
							MarkdownDescription: "The file's name: `invoice.pdf`.",
							Required:            true,
						},
						"content_base64": schema.StringAttribute{
							MarkdownDescription: "The file's bytes in base64: `filebase64(\"invoice.pdf\")`.",
							Required:            true,
						},
						"content_type": schema.StringAttribute{
							MarkdownDescription: "Its type: `application/pdf`; `application/octet-stream` unless set.",
							Optional:            true,
						},
					},
				},
			},
			"idempotency_key": schema.StringAttribute{
				MarkdownDescription: "A key of the caller's: sent again with it, it answers the same email instead of sending twice.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 256)},
			},
		},
	}
}

func (a *sendEmailAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var m sendEmailModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !a.ready(resp) {
		return
	}
	list := func(v types.List) []string {
		if s := stringsOf(ctx, v, &resp.Diagnostics); s != nil {
			return *s
		}
		return nil
	}
	e := yet.Email{
		From:           m.From.ValueString(),
		To:             list(m.To),
		Cc:             list(m.Cc),
		Bcc:            list(m.Bcc),
		ReplyTo:        list(m.ReplyTo),
		Subject:        m.Subject.ValueString(),
		HTML:           m.HTML.ValueString(),
		Text:           m.Text.ValueString(),
		IdempotencyKey: m.IdempotencyKey.ValueString(),
	}
	if known(m.Headers) {
		resp.Diagnostics.Append(m.Headers.ElementsAs(ctx, &e.Headers, false)...)
	}
	if known(m.Tags) {
		tags := map[string]string{}
		resp.Diagnostics.Append(m.Tags.ElementsAs(ctx, &tags, false)...)
		// By name, so the same config sends the same tags in the same order.
		for _, name := range slices.Sorted(maps.Keys(tags)) {
			e.Tags = append(e.Tags, yet.EmailTag{Name: name, Value: tags[name]})
		}
	}
	for _, at := range m.Attachments {
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(at.ContentBase64.ValueString()))
		if err != nil {
			resp.Diagnostics.AddError("Not base64", at.Filename.ValueString()+"'s content_base64 is not base64: "+err.Error())
			return
		}
		e.Attachments = append(e.Attachments, yet.Attachment{
			Filename: at.Filename.ValueString(), Content: b, ContentType: at.ContentType.ValueString(),
		})
	}
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := a.client.Settings.Sending.Send(ctx, e)
	if err != nil {
		failed(&resp.Diagnostics, "send the email", err)
		return
	}
	resp.SendProgress(action.InvokeProgressEvent{Message: "Sent: " + id})
}
