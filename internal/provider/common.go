package provider

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	yet "github.com/yetlabs/yet-sdk-go"
)

// base is what every resource and data source shares: the SDK's client,
// handed over by the provider's Configure.
type base struct {
	client *yet.Client
}

func (b *base) configure(data any, diags *diag.Diagnostics) {
	if data == nil {
		return
	}
	client, ok := data.(*yet.Client)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("Expected *yet.Client, got %T.", data))
		return
	}
	b.client = client
}

func (b *base) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	b.configure(req.ProviderData, &resp.Diagnostics)
}

type dataBase struct{ base }

func (b *dataBase) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	b.configure(req.ProviderData, &resp.Diagnostics)
}

// failed adds yet's refusal, in its own words, to what Terraform shows.
func failed(diags *diag.Diagnostics, doing string, err error) {
	diags.AddError("Could not "+doing, err.Error())
}

// gone is a read that found nothing: the resource leaves the state, and the
// next plan makes it again — what Terraform expects of a thing deleted outside.
func gone(ctx context.Context, err error, resp *resource.ReadResponse) bool {
	if yet.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return true
	}
	return false
}

// known is whether a value was given: not null, and not to be known after apply.
func known(v attr.Value) bool { return !v.IsNull() && !v.IsUnknown() }

func strPtr(v types.String) *string {
	if !known(v) {
		return nil
	}
	s := v.ValueString()
	return &s
}

func intPtr(v types.Int64) *int {
	if !known(v) {
		return nil
	}
	i := int(v.ValueInt64())
	return &i
}

func boolPtr(v types.Bool) *bool {
	if !known(v) {
		return nil
	}
	b := v.ValueBool()
	return &b
}

// strings is a list or set of strings as Go has them; nil when not given.
func stringsOf(ctx context.Context, v interface {
	attr.Value
	ElementsAs(context.Context, any, bool) diag.Diagnostics
}, diags *diag.Diagnostics) *[]string {
	if !known(v) {
		return nil
	}
	out := []string{}
	diags.Append(v.ElementsAs(ctx, &out, false)...)
	return &out
}

func listOf(ctx context.Context, s []string, diags *diag.Diagnostics) types.List {
	if s == nil {
		s = []string{}
	}
	l, d := types.ListValueFrom(ctx, types.StringType, s)
	diags.Append(d...)
	return l
}

func setOf(ctx context.Context, s []string, diags *diag.Diagnostics) types.Set {
	if s == nil {
		s = []string{}
	}
	l, d := types.SetValueFrom(ctx, types.StringType, s)
	diags.Append(d...)
	return l
}

func mapOf(ctx context.Context, m map[string]string, diags *diag.Diagnostics) types.Map {
	if m == nil {
		m = map[string]string{}
	}
	v, d := types.MapValueFrom(ctx, types.StringType, m)
	diags.Append(d...)
	return v
}

// orNull is a string Cloud keeps, or null when it keeps none.
func orNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// diagsSink gathers diagnostics where a function answers with an error.
type diagsSink struct{ d diag.Diagnostics }

func (s *diagsSink) err() error {
	if !s.d.HasError() {
		return nil
	}
	return fmt.Errorf("%s: %s", s.d.Errors()[0].Summary(), s.d.Errors()[0].Detail())
}

func joinTicks(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += "`, `"
		}
		out += x
	}
	return out
}

// nameChars is a mailbox's name; kubeName, Cloud's names — they become
// Kubernetes names, an environment's namespace yet-<name>.
var (
	nameChars = regexp.MustCompile(`^[a-z0-9._-]+$`)
	kubeName  = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,30}[a-z0-9])?$`)
)

const kubeNameRule = "lowercase letters, digits and dashes, up to 32, starting with a letter"

// colors are what Cloud draws environments and pools with.
var colors = []string{"gray", "red", "orange", "amber", "green", "teal", "sky", "blue", "violet", "pink"}
