package immuta

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/instacart/terraform-provider-immuta/client"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &DataSourceResource{}
var _ resource.ResourceWithImportState = &DataSourceResource{}

func NewDataSourceResource() resource.Resource {
	return &DataSourceResource{}
}

// DataSourceResource defines the resource implementation.
type DataSourceResource struct {
	client *client.ImmutaClient
}

// DataSourceResourceModel describes the resource data model.
type DataSourceResourceModel struct {
	Id            types.String `tfsdk:"id"`
	ConnectionKey types.String `tfsdk:"connection_key"`
	NameTemplate  struct {
		DataSourceFormat        types.String `tfsdk:"data_source_format"`
		TableFormat             types.String `tfsdk:"table_format"`
		SchemaFormat            types.String `tfsdk:"schema_format"`
		SchemaProjectNameFormat types.String `tfsdk:"schema_project_name_format"`
	} `tfsdk:"name_template"`
	Options struct {
		TableTags                     []types.String `tfsdk:"table_tags"`
		DisableSensitiveDataDiscovery types.Bool     `tfsdk:"disable_sensitive_data_discovery"`
	} `tfsdk:"options"`
	Owners struct {
		Type types.String `tfsdk:"type"`
		Name types.String `tfsdk:"name"`
		Iam  types.String `tfsdk:"iam"`
	}
	Connection struct {
		Handler                 types.String `tfsdk:"handler"`
		Hostname                types.String `tfsdk:"hostname"`
		Port                    types.Number `tfsdk:"port"`
		Database                types.String `tfsdk:"database"`
		Schema                  types.String `tfsdk:"schema"`
		Username                types.String `tfsdk:"username"`
		AuthenticationMethod    types.String `tfsdk:"authentication_method"`
		Password                types.String `tfsdk:"password"`
		UserFiles               types.List   `tfsdk:"user_files"`
		ConnectionStringOptions types.String `tfsdk:"connection_string_options"`
		Ssl                     types.Bool   `tfsdk:"ssl"`
		Warehouse               types.String `tfsdk:"warehouse"`
		HttpPath                types.String `tfsdk:"http_path"`
	} `tfsdk:"connection"`
}

func (r *DataSourceResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_source"
}

type DataSourceInput struct {
	ConnectionKey string `json:"connectionKey"`
	NameTemplate  struct {
		DataSourceFormat        string `json:"dataSourceFormat"`
		TableFormat             string `json:"tableFormat"`
		SchemaFormat            string `json:"schemaFormat"`
		SchemaProjectNameFormat string `json:"schemaProjectNameFormat"`
	} `json:"nameTemplate"`
	Options struct {
		TableTags                     []string   `json:"tableTags"`
		DisableSensitiveDataDiscovery types.Bool `json:"disableSensitiveDataDiscovery"`
	} `json:"options"`
	Owners struct {
		Type types.String `json:"type"`
		Name types.String `json:"name"`
		Iam  types.String `json:"iam"`
	}
	Connection struct {
		Handler              string `json:"handler"`
		Hostname             string `json:"hostname"`
		Port                 int    `json:"port"`
		Database             string `json:"database"`
		Schema               string `json:"schema"`
		Username             string `json:"username"`
		AuthenticationMethod string `json:"authenticationMethod"`
		Password             string `json:"password"`
		UserFiles            []struct {
			Key      string `json:"key"`
			Content  string `json:"content"`
			FileName string `json:"fileName"`
		} `json:"userFiles"`
		ConnectionStringOptions string `json:"connectionStringOptions"`
		Ssl                     bool   `json:"ssl"`
		Warehouse               string `json:"warehouse"`
		HttpPath                string `json:"httpPath"`
	} `json:"connection"`
}

func (r *DataSourceResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "Register a data source via the V2 API.",

		Attributes: map[string]schema.Attribute{
			"id": stringResourceId(),
			"connection_key": schema.StringAttribute{
				Required:    true,
				Description: "The connection string key, must be unique.",
			},
			"name_template": schema.SingleNestedAttribute{
				Required:    true,
				Description: "The name template for the data source.",
				Attributes: map[string]schema.Attribute{
					"data_source_format": schema.StringAttribute{
						Required:    true,
						Description: "How the data source named will be formatted in Immuta.",
					},
					"table_format": schema.StringAttribute{
						Required:    true,
						Description: "How the table named will be formatted in Immuta.",
					},
					"schema_format": schema.StringAttribute{
						Required:    true,
						Description: "How the schema named will be formatted in Immuta.",
					},
					"schema_project_name_format": schema.StringAttribute{
						Required:    true,
						Description: "How the schema project named will be formatted in Immuta.",
					},
				},
			},
			"options": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "The options for the data source.",
				Attributes: map[string]schema.Attribute{
					"table_tags": schema.ListAttribute{
						Optional:    true,
						Description: "Tags to be applied to each data source ingested via the connection.",
						ElementType: types.StringType,
					},
					"disableSensitiveDataDiscovery": schema.BoolAttribute{
						Optional:    true,
						Description: "Whether to disable sensitive data discovery for the data source.",
					},
				},
			},
			"owners": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "The owners for the data source.",
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Required:    true,
						Description: "The type of the owner, user or group.",
					},
					"name": schema.StringAttribute{
						Required:    true,
						Description: "The name of the owner.",
					},
					"iam": schema.StringAttribute{
						Optional:    true,
						Description: "The IAM system of the owner.",
					},
				},
			},
			"connection": schema.SingleNestedAttribute{
				Required:    true,
				Description: "The connection details for the data source.",
				Attributes: map[string]schema.Attribute{
					"handler": schema.StringAttribute{
						Required:    true,
						Description: "The handler for the data source, one of [Snowflake, Databricks, Trino etc.].",
					},
					"hostname": schema.StringAttribute{
						Required:    true,
						Description: "The hostname for the data source.",
					},
					"port": schema.NumberAttribute{
						Required:    true,
						Description: "The port to which to connect.",
					},
					"database": schema.StringAttribute{
						Required:    true,
						Description: "The database containing the data source.",
					},
					"schema": schema.StringAttribute{
						Optional:    true,
						Description: "The schema containing the data source.",
					},
					"username": schema.StringAttribute{
						Required:    true,
						Description: "The username with which to connect.",
					},
					"authentication_method": schema.StringAttribute{
						Optional:    true,
						Description: "The authentication method for the data source.",
					},
					"password": schema.StringAttribute{
						Optional:    true,
						Description: "The password for the data source.",
					},
					"user_files": schema.ListNestedAttribute{
						Optional:    true,
						Description: "The user files for the data source.",
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"key": schema.StringAttribute{
									Required:    true,
									Description: "The key for the user file.",
								},
								"content": schema.StringAttribute{
									Required:    true,
									Description: "The base64 encoded content of the user file.",
								},
								"file_name": schema.StringAttribute{
									Required:    true,
									Description: "The file name for the user file, to be displayed in UI.",
								},
							},
						},
					},
					"connection_string_options": schema.StringAttribute{
						Optional:    true,
						Description: "The connection string options for the data source.",
					},
					"ssl": schema.BoolAttribute{
						Optional:    true,
						Description: "Whether or not to use SSL for the data source.",
					},
					"warehouse": schema.StringAttribute{
						Optional:    true,
						Description: "[Snowflake] The warehouse for the ingestion.",
					},
					"httpPath": schema.StringAttribute{
						Optional:    true,
						Description: "[Databricks] The HTTP path for the cluster used to ingest.",
					},
				},
			},
		},
	}
}

func (r *DataSourceResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*client.ImmutaClient)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.ImmutaClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = client
}

func (r *DataSourceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *DataSourceResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// todo
	// Actually create the DataSource

	// todo set the resource id
	// data.Id =

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DataSourceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *DataSourceResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// todo
	// Actually read the DataSource

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DataSourceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *DataSourceResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// todo
	// Actually update the DataSource

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DataSourceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *DataSourceResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// todo
	// Actually delete the DataSource
}

func (r *DataSourceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
