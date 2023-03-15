package immuta

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/instacart/terraform-provider-immuta/client"
	"strings"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &BimAttributeResource{}
var _ resource.ResourceWithImportState = &BimAttributeResource{}

func NewBimAttributeResource() resource.Resource {
	return &BimAttributeResource{}
}

// BimAttributeResource defines the resource implementation.
type BimAttributeResource struct {
	client *client.ImmutaClient
}

// BimAttributeResourceModel describes the resource data model.
type BimAttributeResourceModel struct {
	// https://documentation.immuta.com/SaaS/policy-as-code/v1-api/configure/bim/#remove-a-user-or-groups-attribute
	Id        types.String `tfsdk:"id"`
	IamId     types.String `tfsdk:"iam_id"`
	ModelType types.String `tfsdk:"model_type"`
	ModelId   types.String `tfsdk:"model_id"`
	Key       types.String `tfsdk:"key"`
	Value     types.String `tfsdk:"value"`
}

func (r *BimAttributeResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bim_attribute"
}

func (r *BimAttributeResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "A k/v attribute that can be applied to a user or group.",

		Attributes: map[string]schema.Attribute{
			"id": stringResourceId(),
			"iam_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the user or group to which the attribute will be applied.",
				Required:            true,
			},
			"model_type": schema.StringAttribute{
				MarkdownDescription: "The type of the model to which the attribute will be applied. Must be either 'user' or 'group'.",
				Required:            true,
			},
			"model_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the model (i.e. user or group) to which the attribute will be applied.",
				Required:            true,
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "The key of the attribute.",
				Required:            true,
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "The value of the attribute.",
				Required:            true,
			},
		},
	}
}

func (r *BimAttributeResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	immutaClient, ok := req.ProviderData.(*client.ImmutaClient)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *immutaClient.ImmutaClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = immutaClient
}

func (r *BimAttributeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *BimAttributeResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.CreateBimAttribute(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating BimAttribute",
			fmt.Sprintf("Error creating BimAttribute: %s", err),
		)
		return
	}

	data.Id = types.StringValue(createBimAttributeTerraformId(data))

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BimAttributeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *BimAttributeResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	userAttributes, err := r.GetBimAuthorizations(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading BimAttribute",
			fmt.Sprintf("Error reading BimAttribute: %s", err),
		)
		return
	}

	foundAttribute := false
	for key, value := range userAttributes.BimAuthorizations {
		if key == strings.ToLower(data.Key.ValueString()) {
			for _, v := range value {
				if v == data.Value.ValueString() {
					foundAttribute = true
					break
				}
			}
		}
	}

	if !foundAttribute {
		resp.State.RemoveResource(ctx)
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BimAttributeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *BimAttributeResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// todo as update not currently supported
	resp.Diagnostics.AddError("Update not supported", "Update not supported for BimAttribute resource")

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BimAttributeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *BimAttributeResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.DeleteBimAuthorization(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error deleting BimAttribute",
			fmt.Sprintf("Error deleting BimAttribute: %s", err),
		)
		return
	}
}

func (r *BimAttributeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// No unique ID returned from API, so we'll create one
func createBimAttributeTerraformId(data *BimAttributeResourceModel) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", data.IamId, data.ModelType, data.ModelId, data.Key, data.Value)
}

// CRUD methods

func (r *BimAttributeResource) CreateBimAttribute(ctx context.Context, data *BimAttributeResourceModel) (err error) {
	err = r.client.Put(fmt.Sprintf("/bim/iam/%s/%s/%s/authorizations/%s/%s", data.IamId.ValueString(), data.ModelType.ValueString(), data.ModelId.ValueString(), data.Key.ValueString(), data.Value.ValueString()), "", nil, nil)
	return
}

func (r *BimAttributeResource) GetBimAuthorizations(ctx context.Context, data *BimAttributeResourceModel) (resp *BimAttributeUserResponse, err error) {
	err = r.client.Get(fmt.Sprintf("/bim/iam/%s/%s/%s", data.IamId.ValueString(), data.ModelType.ValueString(), data.ModelId.ValueString()), "", map[string]string{}, &resp)
	return
}

func (r *BimAttributeResource) DeleteBimAuthorization(ctx context.Context, data *BimAttributeResourceModel) (err error) {
	err = r.client.Delete(fmt.Sprintf("/bim/iam/%s/%s/%s/authorizations/%s/%s", data.IamId.ValueString(), data.ModelType.ValueString(), data.ModelId.ValueString(), data.Key.ValueString(), data.Value.ValueString()), "", nil, nil)
	return
}

// Domain-specific methods

type BimAttributeUserResponse struct {
	BimAuthorizations map[string][]string `json:"bimAuthorizations"`
}
