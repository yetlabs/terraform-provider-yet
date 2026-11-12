// Package provider is yet's Terraform provider: every product of a company —
// its Settings, its Cloud — as resources, data sources and actions, through
// yet's Go SDK. Their names say their product, as AWS's do: yet_cloud_server,
// yet_settings_person.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

var (
	_ provider.Provider            = (*yetProvider)(nil)
	_ provider.ProviderWithActions = (*yetProvider)(nil)
)

type yetProvider struct {
	version string
}

// New is the provider at a version — "dev", "test", or a release's.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &yetProvider{version: version} }
}

type providerModel struct {
	APIKey     types.String `tfsdk:"api_key"`
	BaseDomain types.String `tfsdk:"base_domain"`
	CloudURL   types.String `tfsdk:"cloud_url"`
}

func (p *yetProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "yet"
	resp.Version = p.version
}

func (p *yetProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Every product of a Yet company — its Settings, its Cloud — as code.",
		MarkdownDescription: "Every product of a [Yet](https://yet.com.br) company — its Settings, its Cloud — as code. " +
			"An API key, made in Yet in **Preferences → API keys**, names the company and acts as its person: " +
			"the provider does what they may do, and nothing more.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				MarkdownDescription: "The API key, `yet_<company>_<secret>`. Falls back on `YET_API_KEY`.",
				Optional:            true,
				Sensitive:           true,
			},
			"base_domain": schema.StringAttribute{
				MarkdownDescription: "Yet's domain: `yet.com.br` unless set, or `YET_BASE_DOMAIN`.",
				Optional:            true,
			},
			"cloud_url": schema.StringAttribute{
				MarkdownDescription: "Yet's API, every product's: `https://cloud.<base_domain>` unless set, or `YET_CLOUD_URL`.",
				Optional:            true,
			},
		},
	}
}

func (p *yetProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var m providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cfg := yet.ConfigFromEnv()
	set := func(v types.String, to *string) {
		if !v.IsNull() && !v.IsUnknown() {
			*to = v.ValueString()
		}
	}
	set(m.APIKey, &cfg.APIKey)
	set(m.BaseDomain, &cfg.BaseDomain)
	set(m.CloudURL, &cfg.CloudURL)
	if cfg.APIKey == "" {
		resp.Diagnostics.AddError("No API key",
			"Set api_key in the provider, or YET_API_KEY. Make one in Yet: Preferences → API keys.")
		return
	}
	cfg.UserAgent = "terraform-provider-yet/" + p.version
	if tf := os.Getenv("TF_APPEND_USER_AGENT"); tf != "" {
		cfg.UserAgent += " " + tf
	}
	client, err := yet.New(cfg)
	if err != nil {
		resp.Diagnostics.AddError("Invalid API key", err.Error())
		return
	}
	resp.ResourceData = client
	resp.DataSourceData = client
	resp.ActionData = client
}

func (p *yetProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		// Settings
		newWorkspaceResource,
		newWorkspaceAppsResource,
		newPersonResource,
		newDomainResource,
		newDomainClaimResource,
		newMailAliasResource,
		newMailListResource,
		newMailCatchAllResource,
		newSendingResource,
		newAWSAccountResource,
		newAWSAccountRoleResource,
		newStorageResource,
		newLogoResource,
		newSubdomainResource,
		newSharedMailboxResource,
		newSSOAppResource,
		// Cloud
		newRackResource,
		newServerResource,
		newRuntimeResource,
		newPoolResource,
		newEnvironmentResource,
		newNamespaceResource,
		newApplicationResource,
		newDatabaseResource,
		newDNSZoneResource,
		newDNSRecordResource,
		newContainerResource,
		newRunnerResource,
		newGitHubConnectionResource,
		newApplicationAddonResource,
		newDatabaseAttachmentResource,
	}
}

func (p *yetProvider) Actions(context.Context) []func() action.Action {
	return []func() action.Action{
		newDatabaseAdoptAction,
		newDatabaseBackupAction,
		newDatabaseRestoreAction,
		newApplicationReleaseAction,
		newApplicationPromoteAction,
		newApplicationBuildAction,
		newApplicationPodDeleteAction,
		newDNSZoneVerifyAction,
		newSendEmailAction,
	}
}

func (p *yetProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		newEnvironmentDataSource,
		newRuntimeDataSource,
		newDNSZoneDataSource,
		newStacksDataSource,
		newGitHubRepositoriesDataSource,
		newGitHubBranchesDataSource,
		newIAMAccessDataSource,
		newIAMKeysDataSource,
		newAWSLaunchDataSource,
	}
}
