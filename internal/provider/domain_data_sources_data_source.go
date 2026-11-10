package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ datasource.DataSource              = (*domainDataSourcesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*domainDataSourcesDataSource)(nil)
)

var domainDataSourceAttributeTypes = map[string]attr.Type{
	"id":       types.StringType,
	"name":     types.StringType,
	"platform": types.StringType,
}

// NewDomainDataSourcesDataSource returns the immuta_domain_data_sources data
// source.
func NewDomainDataSourcesDataSource() datasource.DataSource {
	return &domainDataSourcesDataSource{}
}

type domainDataSourcesDataSource struct {
	client *immutaapi.Client
}

type domainDataSourcesDataSourceModel struct {
	DomainID    types.String `tfsdk:"domain_id"`
	SearchText  types.String `tfsdk:"search_text"`
	IDs         types.List   `tfsdk:"ids"`
	DataSources types.List   `tfsdk:"data_sources"`
}

func (d *domainDataSourcesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_data_sources"
}

func (d *domainDataSourcesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the Immuta data sources in a domain, with the documented `GET /domain/{domainId}/datasources` endpoint. " +
			"Use it to give `immuta_data_product.data_sources` data source IDs.",
		Attributes: map[string]schema.Attribute{
			"domain_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of the domain, usually `immuta_domain.<name>.id`.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"search_text": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only list the data sources whose name contains this text.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"ids": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "IDs of the data sources, in the order Immuta returns them.",
			},
			"data_sources": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The data sources, in the order Immuta returns them.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":       schema.StringAttribute{Computed: true, MarkdownDescription: "ID of the data source."},
						"name":     schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the data source."},
						"platform": schema.StringAttribute{Computed: true, MarkdownDescription: "Data platform of the data source."},
					},
				},
			},
		},
	}
}

func (d *domainDataSourcesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureDataSourceClient(req, resp)
}

func (d *domainDataSourcesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config domainDataSourcesDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domainID := config.DomainID.ValueString()

	dataSources, err := d.client.ListDomainDataSources(ctx, domainID, config.SearchText.ValueString())
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to list the data sources of the Immuta domain %s", domainID), err))

		return
	}

	ids := make([]string, 0, len(dataSources))
	objects := make([]attr.Value, 0, len(dataSources))
	for _, dataSource := range dataSources {
		ids = append(ids, dataSource.ID)

		object, diags := types.ObjectValue(domainDataSourceAttributeTypes, map[string]attr.Value{
			"id":       types.StringValue(dataSource.ID),
			"name":     types.StringValue(dataSource.Name),
			"platform": types.StringValue(dataSource.Platform),
		})
		resp.Diagnostics.Append(diags...)
		objects = append(objects, object)
	}

	idList, diags := types.ListValueFrom(ctx, types.StringType, ids)
	resp.Diagnostics.Append(diags...)
	dataSourceList, diags := types.ListValue(types.ObjectType{AttrTypes: domainDataSourceAttributeTypes}, objects)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	config.IDs = idList
	config.DataSources = dataSourceList

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
