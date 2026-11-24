package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ datasource.DataSource              = (*dataSourceDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*dataSourceDataSource)(nil)
)

// NewDataSourceDataSource returns the immuta_data_source data source.
func NewDataSourceDataSource() datasource.DataSource {
	return &dataSourceDataSource{}
}

type dataSourceDataSource struct {
	client *immutaapi.Client
}

type dataSourceDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	ConnectionKey types.String `tfsdk:"connection_key"`
	Path          types.List   `tfsdk:"path"`
	DataObjectID  types.String `tfsdk:"data_object_id"`
}

func (d *dataSourceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_source"
}

func (d *dataSourceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing Immuta data source, such as a table, by its exact name or by the connection " +
			"and path it was registered from, with the documented `GET /dataSource` endpoints. Use it to give " +
			"`immuta_domain_data_source_assignment` a data source ID.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Exact name of the data source. Exactly one of `name` and `connection_key` (with " +
					"`path`) is required.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.ExactlyOneOf(path.MatchRoot("name"), path.MatchRoot("connection_key")),
				},
			},
			"connection_key": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Key of the connection the data source was registered from, usually `immuta_connection.<name>.connection_key`. Requires `path`.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.AlsoRequires(path.MatchRoot("path")),
				},
			},
			"path": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				MarkdownDescription: "Names from the top of the connection down to the table, for example " +
					"`[\"acme_dev\", \"sales\", \"orders\"]`. Requires `connection_key`.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
					listvalidator.AlsoRequires(path.MatchRoot("connection_key")),
				},
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ID of the data source.",
			},
			"data_object_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ID of the connection data object of the data source, as `immuta_data_object_settings.<name>.id`.",
			},
		},
	}
}

func (d *dataSourceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureDataSourceClient(req, resp)
}

func (d *dataSourceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config dataSourceDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var found *immutaapi.DataSource
	if name := config.Name.ValueString(); name != "" {
		dataSource, err := d.client.GetDataSourceByName(ctx, name)
		if immutaapi.IsNotFound(err) {
			resp.Diagnostics.AddError("No Immuta data source found", fmt.Sprintf("No Immuta data source you can see is named %q. The match is exact.", name))

			return
		}
		if err != nil {
			resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to look up the Immuta data source %q", name), err))

			return
		}
		found = dataSource
	} else {
		var names []string
		resp.Diagnostics.Append(config.Path.ElementsAs(ctx, &names, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		found = d.findByPath(ctx, config.ConnectionKey.ValueString(), names, resp)
		if found == nil {
			return
		}
	}

	namePath, diags := types.ListValueFrom(ctx, types.StringType, nonNil(found.NamePath))
	resp.Diagnostics.Append(diags...)

	config.ID = types.StringValue(found.ID)
	config.Name = types.StringValue(found.Name)
	config.ConnectionKey = types.StringValue(found.ConnectionKey)
	config.Path = namePath
	config.DataObjectID = types.StringValue(found.DataObjectID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// findByPath searches data sources by the last path name, then keeps the one
// registered from the connection with exactly that path.
func (d *dataSourceDataSource) findByPath(ctx context.Context, connectionKey string, names []string, resp *datasource.ReadResponse) *immutaapi.DataSource {
	description := fmt.Sprintf("the path %s in the connection %s", formatObjectPath(names), connectionKey)

	candidates, err := d.client.SearchDataSources(ctx, immutaapi.SearchDataSourcesOptions{SearchText: names[len(names)-1]})
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Unable to look up the Immuta data source with "+description, err))

		return nil
	}

	var matches []immutaapi.DataSource
	var others []string
	for _, candidate := range candidates {
		if candidate.ConnectionKey == connectionKey && slices.Equal(candidate.NamePath, names) {
			matches = append(matches, candidate)
		} else if candidate.ConnectionKey == connectionKey {
			others = append(others, formatObjectPath(candidate.NamePath))
		}
	}

	switch len(matches) {
	case 1:
		return &matches[0]
	case 0:
		detail := fmt.Sprintf("No Immuta data source you can see has %s. The path is case-sensitive and holds the names "+
			"from the top of the connection down to the table. The lookup searches data source names for %q, so a data "+
			"source renamed without it is only found by name.", description, names[len(names)-1])
		if len(others) > 0 {
			slices.Sort(others)
			detail += fmt.Sprintf(" Paths found in that connection: %s.", strings.Join(others, ", "))
		}
		resp.Diagnostics.AddError("No Immuta data source found", detail)
	default:
		var ids []string
		for _, match := range matches {
			ids = append(ids, match.ID)
		}
		resp.Diagnostics.AddError("Several Immuta data sources found",
			fmt.Sprintf("The data sources %s all have %s. Look the one to use up by name.", strings.Join(ids, ", "), description))
	}

	return nil
}
