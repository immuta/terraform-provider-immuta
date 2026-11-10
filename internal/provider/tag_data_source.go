package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ datasource.DataSource              = (*tagDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*tagDataSource)(nil)
)

// NewTagDataSource returns the immuta_tag data source.
func NewTagDataSource() datasource.DataSource {
	return &tagDataSource{}
}

type tagDataSource struct {
	client *immutaapi.Client
}

type tagDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Source        types.String `tfsdk:"source"`
	SystemCreated types.Bool   `tfsdk:"system_created"`
}

func (d *tagDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tag"
}

func (d *tagDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing Immuta tag by its fully qualified name, with the documented `GET /tag` " +
			"search. Use it to reference tags Terraform does not manage, such as tags under `Discovered` or tags created " +
			"in the Immuta UI. For a tag managed with `immuta_tag`, reference that resource instead.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Fully qualified name of the tag, with a dot separating a parent tag from its child, " +
					"for example `Access.Layer.Gold`. The match is exact and case-sensitive.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Numeric ID of the tag, the same identifier as `immuta_tag.id`.",
			},
			"source": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Where the tag came from, for example `curated`.",
			},
			"system_created": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether Immuta created the tag itself rather than a user creating it.",
			},
		},
	}
}

func (d *tagDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureDataSourceClient(req, resp)
}

func (d *tagDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config tagDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := config.Name.ValueString()

	tag, err := d.client.FindTag(ctx, name)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			resp.Diagnostics.AddError("No Immuta tag found",
				fmt.Sprintf("Immuta has no tag named %q. The name is fully qualified, with dots between parent and child "+
					"tags, and the match is exact and case-sensitive.", name))

			return
		}

		resp.Diagnostics.Append(tagErrorDiagnostic(fmt.Sprintf("Unable to look up the Immuta tag %s", name), err))

		return
	}

	var model tagResourceModel
	model.applyTag(tag)

	config.ID = model.ID
	config.Name = model.Name
	config.Source = model.Source
	config.SystemCreated = model.SystemCreated

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
