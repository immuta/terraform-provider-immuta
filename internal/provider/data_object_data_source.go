package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ datasource.DataSource              = (*dataObjectDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*dataObjectDataSource)(nil)
)

// NewDataObjectDataSource returns the immuta_data_object data source.
func NewDataObjectDataSource() datasource.DataSource {
	return &dataObjectDataSource{}
}

type dataObjectDataSource struct {
	client *immutaapi.Client
}

type dataObjectDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	ConnectionKey types.String `tfsdk:"connection_key"`
	Path          types.List   `tfsdk:"path"`
	Name          types.String `tfsdk:"name"`
	Type          types.String `tfsdk:"type"`
}

func (d *dataObjectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_object"
}

func (d *dataObjectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a data object (a catalog, database, schema or table) that a connection registered, by " +
			"its path, with the documented connections API. Use its `id` wherever a data object ID is needed, such as " +
			"`immuta_data_object_tag`.\n\n" +
			"A connection registers its data objects asynchronously, so a data object that is not there yet is waited " +
			"for, up to 10 minutes, before the lookup fails.",
		Attributes: map[string]schema.Attribute{
			"connection_key": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Key of the connection that holds the data object, usually " +
					"`immuta_connection.<name>.connection_key`, so that the data object is looked up after the connection " +
					"is registered.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"path": schema.ListAttribute{
				ElementType: types.StringType,
				Required:    true,
				MarkdownDescription: "Names from the top of the connection down to the data object, for example " +
					"`[\"acme_dev\"]` for a catalog or `[\"acme_dev\", \"sales\", \"orders\"]` for a table. The match is exact " +
					"and case-sensitive.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ID of the data object.",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Name of the data object, the last element of `path`.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Type of the data object, for example `CATALOG`, `SCHEMA` or `TABLE`.",
			},
		},
	}
}

func (d *dataObjectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureDataSourceClient(req, resp)
}

func (d *dataObjectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config dataObjectDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var names []string
	resp.Diagnostics.Append(config.Path.ElementsAs(ctx, &names, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	object, diags := findDataObject(ctx, d.client, config.ConnectionKey.ValueString(), names)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	config.ID = types.StringValue(object.ID)
	config.Name = types.StringValue(object.Name)
	config.Type = types.StringValue(object.Type)

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
