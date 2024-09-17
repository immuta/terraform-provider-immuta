package immuta

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/immuta/terraform-provider-immuta/client"
	"strings"
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
	NameTemplate  types.Object `tfsdk:"name_template"`
	Options       types.Object `tfsdk:"options"`
	Owners        types.List   `tfsdk:"owners"`
	// appended _details because "connection" is a reserved word in HCL
	Connection types.Object `tfsdk:"connection_details"`
}

func (*DataSourceResourceModel) NameTemplateAttributes() map[string]attr.Type {
	return map[string]attr.Type{
		"data_source_format":         types.StringType,
		"table_format":               types.StringType,
		"schema_format":              types.StringType,
		"schema_project_name_format": types.StringType,
	}
}

func (*DataSourceResourceModel) OptionsAttributes() map[string]attr.Type {
	return map[string]attr.Type{
		"table_tags":                       types.ListType{ElemType: types.StringType},
		"disable_sensitive_data_discovery": types.BoolType,
	}
}

func (*DataSourceResourceModel) OwnersAttributes() map[string]attr.Type {
	return map[string]attr.Type{
		"type": types.StringType,
		"name": types.StringType,
		"iam":  types.StringType,
	}
}

func (m *DataSourceResourceModel) ConnectionAttributes() map[string]attr.Type {
	return map[string]attr.Type{
		"handler":                   types.StringType,
		"hostname":                  types.StringType,
		"port":                      types.NumberType,
		"database":                  types.StringType,
		"schema":                    types.StringType,
		"username":                  types.StringType,
		"authentication_method":     types.StringType,
		"password":                  types.StringType,
		"user_files":                types.ListType{ElemType: types.ObjectType{AttrTypes: m.UserFilesAttributes()}},
		"connection_string_options": types.StringType,
		"ssl":                       types.BoolType,
		"warehouse":                 types.StringType,
		"http_path":                 types.StringType,
	}
}

func (*DataSourceResourceModel) UserFilesAttributes() map[string]attr.Type {
	return map[string]attr.Type{
		"key":       types.StringType,
		"content":   types.StringType,
		"file_name": types.StringType,
	}
}

func (r *DataSourceResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_source"
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
				Optional:    true,
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
					"disable_sensitive_data_discovery": schema.BoolAttribute{
						Optional:    true,
						Description: "true|false whether to disable sensitive data discovery for the data source.",
					},
				},
			},
			"owners": schema.ListNestedAttribute{
				Optional:            true,
				MarkdownDescription: "The owners for the data source.",
				NestedObject: schema.NestedAttributeObject{
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
			},
			// appended _details because "connection" is a reserved word in HCL
			"connection_details": schema.SingleNestedAttribute{
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
						Sensitive:   true,
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
									Sensitive:   true,
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
						Description: "true|false Whether or not to use SSL for the data source.",
					},
					"warehouse": schema.StringAttribute{
						Optional:    true,
						Description: "[Snowflake] The warehouse for the ingestion.",
					},
					"http_path": schema.StringAttribute{
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

	dataSourceInput := DataSourceInput{}
	if diags := dataSourceInputFromResourceData(ctx, *data, &dataSourceInput); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	_, err := r.UpsertDataSource(dataSourceInput)
	if err != nil {
		resp.Diagnostics.AddError("Error creating data source", err.Error())
		return
	}

	// todo once can figure out a gettable ID, change to this?
	data.Id = data.ConnectionKey

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Cannot currently read all attributes of a data source created via the V2 API, so we just check if the data source
// still exists and assume the attributes are unchanged
// todo update once a full read method is possible
func (r *DataSourceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *DataSourceResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	doesExist, err := r.ConfirmDataSourceExists(data.ConnectionKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading data source", err.Error())
		return
	}
	if !doesExist {
		resp.State.RemoveResource(ctx)
	}

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

	dataSourceInput := DataSourceInput{}
	if diags := dataSourceInputFromResourceData(ctx, *data, &dataSourceInput); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	_, err := r.UpsertDataSource(dataSourceInput)
	if err != nil {
		resp.Diagnostics.AddError("Error updating data source", err.Error())
		return
	}

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

	err := r.DeleteDataSource(data.ConnectionKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error deleting data source", err.Error())
		return
	}
}

func (r *DataSourceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// helper functions

func dataSourceInputFromResourceData(ctx context.Context, data DataSourceResourceModel, input *DataSourceInput) diag.Diagnostics {
	var diags diag.Diagnostics

	input.ConnectionKey = data.ConnectionKey.ValueString()
	nameTemplate := DataSourceNameTemplate{}
	if conversionDiag := data.NameTemplate.As(ctx, &nameTemplate, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    false,
		UnhandledUnknownAsEmpty: false,
	}); conversionDiag.HasError() {
		return conversionDiag
	}
	input.NameTemplate = nameTemplate

	if !data.Options.IsNull() && !data.Options.IsUnknown() {
		options := DataSourceOptions{}
		if conversionDiag := data.Options.As(ctx, &options, defaultToZeroValue()); conversionDiag.HasError() {
			return conversionDiag
		}
		input.Options = options
	}

	if !data.Owners.IsNull() && !data.Owners.IsUnknown() {
		var owners []DataSourceOwners
		if conversionDiag := data.Owners.ElementsAs(ctx, &owners, false); conversionDiag.HasError() {
			return conversionDiag
		}
		input.Owners = owners
	}

	connection := DataSourceConnection{}
	if conversionDiag := data.Connection.As(ctx, &connection, defaultToZeroValue()); conversionDiag.HasError() {
		return conversionDiag
	}
	input.Connection = connection

	return diags
}

// CRUD methods

func (r *DataSourceResource) UpsertDataSource(dataSource DataSourceInput) (dataSourceResponse DataSourceResponse, err error) {
	err = r.client.PostWithQuery("/api/v2/data", "", dataSource, map[string]string{"dryRun": "false"}, &dataSourceResponse)
	return
}

func (r *DataSourceResource) DeleteDataSource(connectionKey string) (err error) {
	err = r.client.Delete(fmt.Sprintf("/api/v2/data/%s", connectionKey), "", nil, nil)
	return
}

func (r *DataSourceResource) ConfirmDataSourceExists(connectionKey string) (doesExist bool, err error) {
	dataSourceResponse := DataSourceResponse{}
	err = r.client.DeleteWithQuery(
		fmt.Sprintf("/api/v2/data/%s", connectionKey),
		"",
		nil,
		map[string]string{"dryRun": "true"},
		&dataSourceResponse,
	)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Domain specific types

type DataSourceNameTemplate struct {
	DataSourceFormat        string `json:"dataSourceFormat" tfsdk:"data_source_format"`
	TableFormat             string `json:"tableFormat" tfsdk:"table_format"`
	SchemaFormat            string `json:"schemaFormat" tfsdk:"schema_format"`
	SchemaProjectNameFormat string `json:"schemaProjectNameFormat" tfsdk:"schema_project_name_format"`
}

type DataSourceOptions struct {
	TableTags                     []string `json:"tableTags,omitempty" tfsdk:"table_tags"`
	DisableSensitiveDataDiscovery bool     `json:"disableSensitiveDataDiscovery,omitempty" tfsdk:"disable_sensitive_data_discovery"`
}

type DataSourceOwners struct {
	Type string `json:"type" tfsdk:"type"`
	Name string `json:"name" tfsdk:"name"`
	Iam  string `json:"iam,omitempty" tfsdk:"iam"`
}

type DataSourceConnection struct {
	Handler                 string      `json:"handler" tfsdk:"handler"`
	Hostname                string      `json:"hostname" tfsdk:"hostname"`
	Port                    int         `json:"port,omitempty" tfsdk:"port"`
	Database                string      `json:"database" tfsdk:"database"`
	Schema                  string      `json:"schema,omitempty" tfsdk:"schema"`
	Username                string      `json:"username" tfsdk:"username"`
	AuthenticationMethod    string      `json:"authenticationMethod,omitempty" tfsdk:"authentication_method"`
	Password                string      `json:"password,omitempty" tfsdk:"password"`
	UserFiles               []UserFiles `json:"userFiles,omitempty" tfsdk:"user_files"`
	ConnectionStringOptions string      `json:"connectionStringOptions,omitempty" tfsdk:"connection_string_options"`
	Ssl                     bool        `json:"ssl,omitempty" tfsdk:"ssl"`
	Warehouse               string      `json:"warehouse,omitempty" tfsdk:"warehouse"`
	HttpPath                string      `json:"httpPath,omitempty" tfsdk:"http_path"`
}

type UserFiles struct {
	Key      string `json:"key"`
	Content  string `json:"content"`
	FileName string `json:"fileName"`
}

type DataSourceInput struct {
	ConnectionKey string                 `json:"connectionKey"`
	NameTemplate  DataSourceNameTemplate `json:"nameTemplate,omitempty"`
	Options       DataSourceOptions      `json:"options,omitempty"`
	Owners        []DataSourceOwners     `json:"owners,omitempty"`
	Connection    DataSourceConnection   `json:"connection"`
}

type DataSourceResponse struct {
	DryRun           bool     `json:"dryRun"`
	Creating         []string `json:"creating"`
	Updating         []string `json:"updating"`
	Deleting         []string `json:"deleting"`
	NoChange         []string `json:"noChange"`
	DetectionRunning bool     `json:"detectionRunning"`
	TagsUpdated      bool     `json:"tagsUpdated"`
}
