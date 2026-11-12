package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

/*
The company's IAM, to read: who has which role, and who holds an API key.
Giving a role and making a key take a person signed in to Yet — never a key,
so never Terraform — and are done in Settings → IAM and Preferences.
*/

// ── data "yet_iam_access" ────────────────────────────────────────────────────

type iamAccessDataSource struct{ dataBase }

func newIAMAccessDataSource() datasource.DataSource { return &iamAccessDataSource{} }

type grantModel struct {
	Email       types.String `tfsdk:"email"`
	Environment types.String `tfsdk:"environment"`
	Role        types.String `tfsdk:"role"`
	GrantedBy   types.String `tfsdk:"granted_by"`
	GrantedAt   types.String `tfsdk:"granted_at"`
}

type iamAccessModel struct {
	ID     types.String `tfsdk:"id"`
	Grants []grantModel `tfsdk:"grants"`
}

func (d *iamAccessDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_access"
}

func (d *iamAccessDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every role given in the company's products: who may do what in Cloud, everywhere or by environment.\n\n" +
			"~> **Note:** Roles are given in Yet, in Settings → IAM, by someone signed in — never with an API key, so never here. " +
			"A leaked key never hands out more than its person had.",
		Attributes: map[string]schema.Attribute{
			"id": computed("The company's slug."),
			"grants": schema.ListNestedAttribute{
				MarkdownDescription: "The roles given.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"email":       computed("Who has it."),
						"environment": computed("Where: an environment's id, or `*` for everywhere."),
						"role":        computed("`viewer`, `developer` or `admin`."),
						"granted_by":  computed("Who gave it."),
						"granted_at":  computed("When (RFC 3339)."),
					},
				},
			},
		},
	}
}

func (d *iamAccessDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	grants, err := d.client.IAM.Access.List(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "read the company's access", err)
		return
	}
	m := iamAccessModel{ID: types.StringValue(d.client.Workspace), Grants: []grantModel{}}
	for _, g := range grants {
		m.Grants = append(m.Grants, grantModel{
			Email:       types.StringValue(g.Email),
			Environment: types.StringValue(g.Environment),
			Role:        types.StringValue(g.Role),
			GrantedBy:   types.StringValue(g.GrantedBy),
			GrantedAt:   types.StringValue(g.GrantedAt.UTC().Format("2006-01-02T15:04:05Z")),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// ── data "yet_iam_api_keys" ──────────────────────────────────────────────────

type iamKeysDataSource struct{ dataBase }

func newIAMKeysDataSource() datasource.DataSource { return &iamKeysDataSource{} }

type keyModel struct {
	ID         types.String `tfsdk:"id"`
	Email      types.String `tfsdk:"email"`
	Name       types.String `tfsdk:"name"`
	Prefix     types.String `tfsdk:"prefix"`
	CreatedAt  types.String `tfsdk:"created_at"`
	LastUsedAt types.String `tfsdk:"last_used_at"`
}

type iamKeysModel struct {
	ID   types.String `tfsdk:"id"`
	Keys []keyModel   `tfsdk:"keys"`
}

func (d *iamKeysDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_api_keys"
}

func (d *iamKeysDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every API key the company's people hold — who, since when, last used when. Only each key's first characters are kept.\n\n" +
			"~> **Note:** Keys are made by each person, in Yet's Preferences — never with another key — and revoked there, or by an admin in Settings → IAM.",
		Attributes: map[string]schema.Attribute{
			"id": computed("The company's slug."),
			"keys": schema.ListNestedAttribute{
				MarkdownDescription: "The keys, the latest first.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":           computed("The key's id."),
						"email":        computed("Its person."),
						"name":         computed("What it is for."),
						"prefix":       computed("Its first characters: `yet_acme_6bffe6`."),
						"created_at":   computed("When it was made (RFC 3339)."),
						"last_used_at": computed("When it was last used (RFC 3339); empty if never."),
					},
				},
			},
		},
	}
}

func (d *iamKeysDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	keys, err := d.client.IAM.Keys.ListAll(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "read the company's API keys", err)
		return
	}
	stamp := "2006-01-02T15:04:05Z"
	m := iamKeysModel{ID: types.StringValue(d.client.Workspace), Keys: []keyModel{}}
	for _, k := range keys {
		used := ""
		if k.LastUsedAt != nil {
			used = k.LastUsedAt.UTC().Format(stamp)
		}
		m.Keys = append(m.Keys, keyModel{
			ID:         types.StringValue(k.ID),
			Email:      types.StringValue(k.Email),
			Name:       types.StringValue(k.Name),
			Prefix:     types.StringValue(k.Prefix),
			CreatedAt:  types.StringValue(k.CreatedAt.UTC().Format(stamp)),
			LastUsedAt: types.StringValue(used),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
