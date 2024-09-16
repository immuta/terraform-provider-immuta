package immuta

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/immuta/terraform-provider-immuta/client"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &BimGroupResource{}
var _ resource.ResourceWithImportState = &BimGroupResource{}

func NewBimGroupResource() resource.Resource {
	return &BimGroupResource{}
}

// BimGroupResource defines the resource implementation.
type BimGroupResource struct {
	client *client.ImmutaClient
}

// BimGroupResourceModel describes the resource data model.
type BimGroupResourceModel struct {
	Id             types.Number `tfsdk:"id"`
	IamId          types.String `tfsdk:"iamid"`
	Name           types.String `tfsdk:"name"`
	Email          types.String `tfsdk:"email"`
	Authorizations types.Map    `tfsdk:"authorizations"`
	Description    types.String `tfsdk:"description"`
}

func (r *BimGroupResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bim_group"
}

func (r *BimGroupResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "A bim group.",

		Attributes: map[string]schema.Attribute{
			"id": numberResourceId(),
			"iamid": schema.StringAttribute{
				MarkdownDescription: "The IAM ID",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The group name",
				Required:            true,
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "The group email",
				Optional:            true,
			},
			"authorizations": schema.MapAttribute{
				Optional:            true,
				MarkdownDescription: "The group's attributes.",
				ElementType:         types.StringType,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "The group description",
				Optional:            true,
			},
		},
	}
}

func (r *BimGroupResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *BimGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *BimGroupResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bimGroupInput := BimGroupInput{
		IamId:       data.IamId.ValueString(),
		Name:        data.Name.ValueString(),
		Email:       data.Email.ValueString(),
		Description: data.Description.ValueString(),
	}

	bimGroupResponse, err := r.CreateBimGroup(bimGroupInput)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating BimGroup",
			fmt.Sprintf("Error creating BimGroup: %s", err),
		)
		return
	}
	data.Id = intToNumberValue(bimGroupResponse.Id)

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BimGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *BimGroupResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bimGroupResponse, err := r.GetBimGroup(data.Id.String())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading BimGroup",
			fmt.Sprintf("Error reading BimGroup: %s", err),
		)
		return
	}
	if intToNumberValue(bimGroupResponse.Id).String() != data.Id.String() {
		resp.Diagnostics.AddError(
			"Error reading BimGroup",
			fmt.Sprintf("Error reading BimGroup, ID has changed, old:[%s] new:[%d]: %s", data.Id.String(), bimGroupResponse.Id, err),
		)
		return
	}

	if bimGroupResponse.IamId != data.IamId.ValueString() {
		resp.Diagnostics.AddError(
			"Error reading BimGroup",
			fmt.Sprintf("Error reading BimGroup, IamId has changed, old:[%s] new:[%s]: %s", data.IamId.ValueString(), bimGroupResponse.IamId, err),
		)
		return
	}

	if data.Name.ValueString() != bimGroupResponse.Name {
		data.Name = types.StringValue(bimGroupResponse.Name)
	}
	if data.Email.ValueString() != bimGroupResponse.Email {
		data.Email = types.StringValue(bimGroupResponse.Email)
	}
	if data.Description.ValueString() != bimGroupResponse.Description {
		data.Description = types.StringValue(bimGroupResponse.Description)
	}

	newAuthorizations, authorizationsDiag := updateMapIfChanged(ctx, data.Authorizations, bimGroupResponse.Authorizations)
	if authorizationsDiag != nil {
		resp.Diagnostics.Append(authorizationsDiag...)
		return
	}
	data.Authorizations = newAuthorizations

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update group details
func (r *BimGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *BimGroupResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bimGroupProfile := BimGroupProfile{}
	bimGroupProfile.Name = data.Name.ValueString()
	bimGroupProfile.Email = data.Email.ValueString()
	bimGroupProfile.Description = data.Description.ValueString()

	bimGroupResponse, err := r.UpdateBimGroup(data.Id.String(), &bimGroupProfile)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error updating BimGroup",
			fmt.Sprintf("Error updating BimGroupProfile: %s", err),
		)
		return
	}

	if intToNumberValue(bimGroupResponse.Id).String() != data.Id.String() {
		resp.Diagnostics.AddError(
			"Error updating BimGroup",
			fmt.Sprintf("Error updating BimGroup, ID has changed, old:[%s] new:[%d]: %s", data.Id.String(), bimGroupResponse.Id, err),
		)
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BimGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *BimGroupResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.DeleteBimGroup(data.Id.String())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error deleting BimGroup",
			fmt.Sprintf("Error deleting BimGroup: %s", err),
		)
		return
	}
}

func (r *BimGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// CRUD methods

func (r *BimGroupResource) GetBimGroup(groupId string) (bimGroupResponse *BimGroup, err error) {
	err = r.client.Get(fmt.Sprintf("/bim/group/%s", groupId), "", nil, &bimGroupResponse)
	return
}

func (r *BimGroupResource) CreateBimGroup(bimGroup BimGroupInput) (bimGroupResponse *BimGroup, err error) {
	err = r.client.Post("/bim/group", "", bimGroup, &bimGroupResponse)
	return
}

func (r *BimGroupResource) DeleteBimGroup(groupId string) (err error) {
	err = r.client.Delete("/bim/group/"+groupId, "", nil, nil)
	return
}

func (r *BimGroupResource) UpdateBimGroup(groupId string, bimGroupProfile *BimGroupProfile) (bimGroupResponse *BimGroup, err error) {
	err = r.client.Put("/bim/group/"+groupId, "", bimGroupProfile, &bimGroupResponse)
	return
}

// Domain specific types

type BimGroupProfile struct {
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	Description string `json:"description,omitempty"`
}

type BimGroupInput struct {
	IamId       string `json:"iamid" tfsdk:"iamid"`
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	Description string `json:"description,omitempty"`
}

type BimGroup struct {
	BimGroupInput
	Id             int                    `json:"id"`
	Authorizations map[string]interface{} `json:"authorizations,omitempty"`
	CreatedAt      time.Time              `json:"createdAt"`
	UpdatedAt      time.Time              `json:"updatedAt"`
}
