package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

/*
yet_settings_logo is the company's logo on its sign-in page, from a file or
from base64. What Yet keeps is compared by its bytes' SHA-256: a file
changed under the same path, or a logo changed in Settings, shows in the
plan.
*/

type logoResource struct{ base }

func newLogoResource() resource.Resource { return &logoResource{} }

var (
	_ resource.ResourceWithModifyPlan = (*logoResource)(nil)

	logoTypes = []string{"image/png", "image/jpeg", "image/webp", "image/gif", "image/svg+xml"}
)

type logoModel struct {
	ID            types.String `tfsdk:"id"`
	Source        types.String `tfsdk:"source"`
	ContentBase64 types.String `tfsdk:"content_base64"`
	ContentType   types.String `tfsdk:"content_type"`
	SHA256        types.String `tfsdk:"sha256"`
}

func (r *logoResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings_logo"
}

func (r *logoResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	exactlyOne := stringvalidator.ExactlyOneOf(path.MatchRoot("source"), path.MatchRoot("content_base64"))
	resp.Schema = schema.Schema{
		MarkdownDescription: "The company's logo on its sign-in page: a PNG, JPEG, WebP, GIF or SVG under 256 KB, " +
			"from a file (`source`) or base64 (`content_base64`).\n\n" +
			"-> **Note:** Destroying it puts Yet's own mark back on the sign-in page.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The company's slug.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"source": schema.StringAttribute{
				MarkdownDescription: "The image's path: `${path.module}/logo.svg`.",
				Optional:            true,
				Validators:          []validator.String{exactlyOne},
			},
			"content_base64": schema.StringAttribute{
				MarkdownDescription: "The image's bytes in base64: `filebase64(\"logo.png\")`.",
				Optional:            true,
				Validators:          []validator.String{exactlyOne},
			},
			"content_type": schema.StringAttribute{
				MarkdownDescription: "The image's type — any of `" + joinTicks(logoTypes) + "`. " +
					"Unset, it is told from the file's extension, or its bytes.",
				Optional:   true,
				Computed:   true,
				Validators: []validator.String{stringvalidator.OneOf(logoTypes...)},
			},
			"sha256": schema.StringAttribute{
				MarkdownDescription: "The SHA-256 of the image's bytes, as Yet keeps them.",
				Computed:            true,
			},
		},
	}
}

// image is the configured logo: its bytes, and its type, given or told.
func image(m logoModel) ([]byte, string, error) {
	var data []byte
	var name string
	switch {
	case known(m.Source):
		name = m.Source.ValueString()
		b, err := os.ReadFile(name)
		if err != nil {
			return nil, "", err
		}
		data = b
	case known(m.ContentBase64):
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(m.ContentBase64.ValueString()))
		if err != nil {
			return nil, "", fmt.Errorf("content_base64 is not base64: %w", err)
		}
		data = b
	default:
		return nil, "", nil
	}
	if known(m.ContentType) {
		return data, m.ContentType.ValueString(), nil
	}
	return data, typeOf(name, data), nil
}

// typeOf tells an image's type by its extension, or by its bytes; an SVG
// is text, so it is told by its start.
func typeOf(name string, data []byte) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	}
	head := bytes.TrimSpace(data[:min(len(data), 512)])
	if bytes.HasPrefix(head, []byte("<svg")) || bytes.HasPrefix(head, []byte("<?xml")) {
		return "image/svg+xml"
	}
	return http.DetectContentType(data)
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ModifyPlan says what the image will be: its type and its digest, so a
// change to the file's bytes is a change to plan.
func (r *logoResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan, config logoModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// A value known only after apply — another resource's file — waits for it.
	if config.Source.IsUnknown() || config.ContentBase64.IsUnknown() || config.ContentType.IsUnknown() {
		return
	}
	plan.ContentType = config.ContentType
	data, contentType, err := image(plan)
	if err != nil {
		resp.Diagnostics.AddError("Could not read the logo", err.Error())
		return
	}
	if data == nil {
		return
	}
	plan.ContentType = types.StringValue(contentType)
	plan.SHA256 = types.StringValue(digest(data))
	resp.Diagnostics.Append(resp.Plan.Set(ctx, plan)...)
}

func (r *logoResource) set(ctx context.Context, plan logoModel) (*logoModel, error) {
	data, contentType, err := image(plan)
	if err != nil {
		return nil, err
	}
	if err := r.client.Settings.Logo.Set(ctx, yet.Logo{ContentType: contentType, Data: data}); err != nil {
		return nil, err
	}
	plan.ID = types.StringValue(r.client.Workspace)
	plan.ContentType = types.StringValue(contentType)
	plan.SHA256 = types.StringValue(digest(data))
	return &plan, nil
}

func (r *logoResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan logoModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m, err := r.set(ctx, plan)
	if err != nil {
		failed(&resp.Diagnostics, "set the logo", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *logoResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state logoModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	logo, err := r.client.Settings.Logo.Get(ctx)
	if err != nil {
		failed(&resp.Diagnostics, "read the logo", err)
		return
	}
	if logo == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.ID = types.StringValue(r.client.Workspace)
	state.ContentType = types.StringValue(logo.ContentType)
	state.SHA256 = types.StringValue(digest(logo.Data))
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *logoResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan logoModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m, err := r.set(ctx, plan)
	if err != nil {
		failed(&resp.Diagnostics, "set the logo", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func (r *logoResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	if err := r.client.Settings.Logo.Remove(ctx); err != nil && !yet.IsNotFound(err) {
		failed(&resp.Diagnostics, "remove the logo", err)
	}
}

// ImportState takes hold of the logo there is; the next plan sets it from the configuration.
func (r *logoResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
