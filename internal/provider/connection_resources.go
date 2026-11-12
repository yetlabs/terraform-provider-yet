package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

/*
Connections: the company's accounts elsewhere that Cloud uses — GitHub, for
its runners. A token is the analog of what a person does in Cloud's
Connections; installing yet's GitHub App instead takes a person on GitHub,
so it stays the apps'.
*/

// ── yet_settings_connection_github ───────────────────────────────────────────

type githubConnectionResource struct{ base }

func newGitHubConnectionResource() resource.Resource { return &githubConnectionResource{} }

type githubConnectionModel struct {
	ID      types.String `tfsdk:"id"`
	Token   types.String `tfsdk:"token"`
	Account types.String `tfsdk:"account"`
}

func (r *githubConnectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_connection_github"
}

func (r *githubConnectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The company's GitHub, connected to Cloud with a token: what its runners — " +
			"`yet_cloud_runner` — register with when they have no token of their own. GitHub is asked whose " +
			"the token is as it is given, so a wrong one is refused at once. One per company.\n\n" +
			"!> **Warning:** A token takes the place of yet's GitHub App, if it was installed — and the App is what " +
			"applications build from their repositories with. Destroyed, the connection goes, App and all. " +
			"A company that builds from GitHub installs the App in Yet's apps, under Cloud's Connections, instead.\n\n" +
			"-> **Note:** The token is sealed in Cloud and never shown again: an imported connection has none in its " +
			"state until it is set.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Always `github`.",
				Computed:            true,
				PlanModifiers:       keep,
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "A GitHub token that manages the company's runners: a fine-grained one with " +
					"*Self-hosted runners* on the organization, or *Administration: read and write* on a repository.",
				Required:  true,
				Sensitive: true,
			},
			"account": schema.StringAttribute{
				MarkdownDescription: "Whose the token is, as GitHub says.",
				Computed:            true,
			},
		},
	}
}

func (r *githubConnectionResource) set(ctx context.Context, plan *githubConnectionModel, diags *diag.Diagnostics) bool {
	cn, err := r.client.Settings.Connections.GitHub.SetToken(ctx, plan.Token.ValueString())
	if err != nil {
		failed(diags, "connect GitHub", err)
		return false
	}
	plan.ID = types.StringValue(cn.Kind)
	plan.Account = types.StringValue(cn.Account)
	return true
}

func (r *githubConnectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan githubConnectionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || !r.set(ctx, &plan, &resp.Diagnostics) {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *githubConnectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state githubConnectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cn, err := r.client.Settings.Connections.GitHub.Get(ctx)
	if err != nil {
		if !gone(ctx, err, resp) {
			failed(&resp.Diagnostics, "read the GitHub connection", err)
		}
		return
	}
	if !cn.Connected {
		resp.State.RemoveResource(ctx)
		return
	}
	state.ID = types.StringValue(cn.Kind)
	state.Account = types.StringValue(cn.Account)
	// The App installed in its place: no token any more, so the next plan sets it again.
	if len(cn.Accounts) > 0 {
		state.Token = types.StringValue("")
	}
	if state.Token.IsNull() {
		state.Token = types.StringValue("")
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *githubConnectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan githubConnectionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || !r.set(ctx, &plan, &resp.Diagnostics) {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *githubConnectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if err := r.client.Settings.Connections.GitHub.Remove(ctx); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "disconnect GitHub", err)
	}
}

// ImportState takes "github", the only kind there is.
func (r *githubConnectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "github" {
		resp.Diagnostics.AddError("Unexpected import id", "Import the GitHub connection as github.")
		return
	}
	importID(ctx, req, resp)
}

// ── data "yet_settings_github_repositories" ──────────────────────────────────

type githubRepositoriesDataSource struct{ dataBase }

func newGitHubRepositoriesDataSource() datasource.DataSource { return &githubRepositoriesDataSource{} }

type githubRepositoriesModel struct {
	Repositories []githubRepositoryModel `tfsdk:"repositories"`
}

type githubRepositoryModel struct {
	FullName      types.String `tfsdk:"full_name"`
	Private       types.Bool   `tfsdk:"private"`
	DefaultBranch types.String `tfsdk:"default_branch"`
}

func (d *githubRepositoriesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_github_repositories"
}

func (d *githubRepositoriesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		MarkdownDescription: "The repositories yet's GitHub App was given on the company's organization: " +
			"what an application's `repository` can be.\n\n" +
			"-> **Note:** It needs the App installed — in Yet's apps, under Cloud's Connections. A token " +
			"(`yet_settings_connection_github`) is for runners, and lists none.",
		Attributes: map[string]dschema.Attribute{
			"repositories": dschema.ListNestedAttribute{
				MarkdownDescription: "Each repository.",
				Computed:            true,
				NestedObject: dschema.NestedAttributeObject{
					Attributes: map[string]dschema.Attribute{
						"full_name":      computed("`owner/name`, as an application's `repository` takes it."),
						"private":        dschema.BoolAttribute{Computed: true, MarkdownDescription: "Whether it is private."},
						"default_branch": computed("Its default branch."),
					},
				},
			},
		},
	}
}

func (d *githubRepositoriesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	repos, err := d.client.Settings.Connections.GitHub.Repositories(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "list GitHub's repositories", err)
		return
	}
	m := githubRepositoriesModel{Repositories: []githubRepositoryModel{}}
	for _, r := range repos {
		m.Repositories = append(m.Repositories, githubRepositoryModel{
			FullName: types.StringValue(r.FullName), Private: types.BoolValue(r.Private), DefaultBranch: types.StringValue(r.DefaultBranch),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// ── data "yet_settings_github_branches" ──────────────────────────────────────

type githubBranchesDataSource struct{ dataBase }

func newGitHubBranchesDataSource() datasource.DataSource { return &githubBranchesDataSource{} }

type githubBranchesModel struct {
	Repository types.String `tfsdk:"repository"`
	Branches   []string     `tfsdk:"branches"`
}

func (d *githubBranchesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_github_branches"
}

func (d *githubBranchesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		MarkdownDescription: "A repository's branches, one of those yet's GitHub App was given on the company's " +
			"organization: what an application's `branch` can be.\n\n" +
			"-> **Note:** It needs the App installed — in Yet's apps, under Cloud's Connections.",
		Attributes: map[string]dschema.Attribute{
			"repository": dschema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The repository, `owner/name`, as `yet_settings_github_repositories` lists it.",
			},
			"branches": dschema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "Each branch, by name.",
			},
		},
	}
}

func (d *githubBranchesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m githubBranchesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	branches, err := d.client.Settings.Connections.GitHub.Branches(ctx, m.Repository.ValueString())
	if err != nil {
		failed(&resp.Diagnostics, "list the repository's branches", err)
		return
	}
	m.Branches = []string{}
	for _, b := range branches {
		m.Branches = append(m.Branches, b.Name)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
