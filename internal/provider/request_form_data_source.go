package provider

import (
	"context"
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ datasource.DataSource              = (*requestFormDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*requestFormDataSource)(nil)
)

// NewRequestFormDataSource returns the immuta_request_form data source.
func NewRequestFormDataSource() datasource.DataSource {
	return &requestFormDataSource{}
}

type requestFormDataSource struct {
	client *immutaapi.Client
}

type requestFormDataSourceModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

func (d *requestFormDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_request_form"
}

func (d *requestFormDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing Request app (formerly Marketplace) request form by name, with the documented " +
			"`GET /api/request-form` search, for example for `immuta_data_product.data_access_request_form_id`.\n\n" +
			"Needs the provider's `request_app_url` and `request_app_token`. The match is exact and case-sensitive.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the request form, for example `Standard Request Form`. The match is exact and case-sensitive.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ID of the request form.",
			},
		},
	}
}

func (d *requestFormDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureDataSourceClient(req, resp)
	requireRequestApp(d.client, "immuta_request_form", &resp.Diagnostics)
}

func (d *requestFormDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config requestFormDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := config.Name.ValueString()

	forms, err := d.client.SearchRequestForms(ctx, name)
	if err != nil {
		resp.Diagnostics.Append(requestAppErrorDiagnostic(fmt.Sprintf("Unable to look up the Immuta request form %q", name), err))

		return
	}

	matches := exactRequestFormMatches(forms, name)
	switch len(matches) {
	case 0:
		resp.Diagnostics.AddError("No Immuta request form found",
			fmt.Sprintf("No Request app request form is named %q. The match is exact and case-sensitive.", name))

		return
	case 1:
	default:
		resp.Diagnostics.AddError("Several Immuta request forms found",
			fmt.Sprintf("%d Request app request forms are named %q; rename them so that the name is unique.", len(matches), name))

		return
	}

	config.ID = types.StringValue(matches[0].ID)
	config.Name = types.StringValue(matches[0].Name)

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// exactRequestFormMatches keeps the request forms named exactly name. Forms
// without an id are unusable and dropped.
func exactRequestFormMatches(forms []immutaapi.RequestForm, name string) []immutaapi.RequestForm {
	return slices.DeleteFunc(slices.Clone(forms), func(form immutaapi.RequestForm) bool {
		return form.ID == "" || form.Name != name
	})
}
