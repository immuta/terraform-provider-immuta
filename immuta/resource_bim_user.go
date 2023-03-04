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
var _ resource.Resource = &BimUserResource{}
var _ resource.ResourceWithImportState = &BimUserResource{}

func NewBimUserResource() resource.Resource {
	return &BimUserResource{}
}

// BimUserResource defines the resource implementation.
type BimUserResource struct {
	client *client.ImmutaClient
}

// BimUserResourceModel describes the resource data model.
type BimUserResourceModel struct {
	Id            types.String `tfsdk:"id"`
	Userid        types.String `tfsdk:"userid"`
	Password      types.String `tfsdk:"password"`
	Name          types.String `tfsdk:"name"`
	Email         types.String `tfsdk:"email"`
	SnowflakeUser types.String `tfsdk:"snowflake_user"`
}

func (r *BimUserResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bim_user"
}

func (r *BimUserResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "A machine generated user for programmatic access.",

		Attributes: map[string]schema.Attribute{
			"id": stringResourceId(),
			"userid": schema.StringAttribute{
				MarkdownDescription: "",
				Required:            true,
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "",
				Optional:            true,
				Computed:            true,
				Sensitive:           true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "",
				Optional:            true,
				Computed:            true,
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "",
				Required:            true,
			},
			"snowflake_user": schema.StringAttribute{
				MarkdownDescription: "",
				Optional:            true,
			},
		},
	}
}

func (r *BimUserResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *BimUserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *BimUserResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if data.Name.ValueString() == "" {
		data.Name = data.Userid
	}

	bimUserInput := BimUserInput{
		Userid:   data.Userid.ValueString(),
		Password: data.Password.ValueString(),
		Profile: BimUserProfileInput{
			Name:  data.Name.ValueString(),
			Email: data.Email.ValueString(),
		},
	}

	bimUserResponse, err := r.CreateBimUser(bimUserInput)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating BimUser",
			fmt.Sprintf("Error creating BimUser: %s", err),
		)
		return
	}

	if data.SnowflakeUser.ValueString() != "" {
		bimUserProfile := BimUserProfile{}
		bimUserProfile.ExternalUserIds.SnowflakeUser = data.SnowflakeUser.ValueString()

		_, err := r.UpdateBimUserProfile(bimUserInput.Userid, &bimUserProfile)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error creating BimUserProfile",
				fmt.Sprintf("Error updating ExternalUserIds: %s", err),
			)
			return
		}
	}

	data.Id = types.StringValue(bimUserResponse.NewUser.Userid)

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BimUserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *BimUserResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bimUserResponse, err := r.GetBimUser(data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading BimUser",
			fmt.Sprintf("Error reading BimUser: %s", err),
		)
		return
	}
	if bimUserResponse.Userid != data.Id.ValueString() {
		resp.Diagnostics.AddError(
			"Error reading BimUser",
			fmt.Sprintf("Error reading BimUser, ID has changed, old:[%s] new:[%s]: %s", data.Id.ValueString(), bimUserResponse.Userid, err),
		)
		return
	}

	if data.Name.ValueString() != bimUserResponse.Profile.Name {
		data.Name = types.StringValue(bimUserResponse.Profile.Name)
	}
	if data.Email.ValueString() != bimUserResponse.Profile.Email {
		data.Email = types.StringValue(bimUserResponse.Profile.Email)
	}
	if data.SnowflakeUser.ValueString() != bimUserResponse.Profile.ExternalUserIds.SnowflakeUser {
		data.SnowflakeUser = types.StringValue(bimUserResponse.Profile.ExternalUserIds.SnowflakeUser)
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update updates user profile details
func (r *BimUserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *BimUserResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if data.Name.ValueString() != "" && data.Name.ValueString() != data.Userid.ValueString() {
		resp.Diagnostics.AddError(
			"Error updating BimUser",
			fmt.Sprintf("Error updating BimUser, Name cannot be changed: %s", data.Name.ValueString()),
		)
		return
	}
	if data.Name.ValueString() == "" {
		data.Name = data.Userid
	}

	bimUserProfile := BimUserProfile{}
	bimUserProfile.Email = data.Email.ValueString()
	bimUserProfile.ExternalUserIds.SnowflakeUser = data.SnowflakeUser.ValueString()

	_, err := r.UpdateBimUserProfile(data.Id.ValueString(), &bimUserProfile)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error updating BimUser",
			fmt.Sprintf("Error updating BimUserProfile: %s", err),
		)
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BimUserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *BimUserResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.DeleteBimUser(data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error deleting BimUser",
			fmt.Sprintf("Error deleting BimUser: %s", err),
		)
		return
	}
}

func (r *BimUserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// CRUD methods

func (r *BimUserResource) ListBimUsers() (bimUserResponse *BimUser, err error) {
	err = r.client.Get("/bim/iam/bim/user", "", map[string]string{}, &bimUserResponse)
	return
}

func (r *BimUserResource) GetBimUser(userid string) (bimUserResponse *BimUser, err error) {
	err = r.client.Get("/bim/iam/bim/user/"+userid, "", map[string]string{}, &bimUserResponse)
	return
}

func (r *BimUserResource) CreateBimUser(bimUser BimUserInput) (bimUserResponse *BimUserCreateResponse, err error) {
	err = r.client.Post("/bim/iam/bim/user", "", bimUser, &bimUserResponse)
	return
}

func (r *BimUserResource) DeleteBimUser(userid string) (err error) {
	err = r.client.Delete("/bim/iam/bim/user/"+userid, "", nil, nil)
	return
}

//func (a *BimUserResource) UpdateBimUser(userid string, profile *BimUserProfile) (bimUserResponse *BimUser, err error) {
//	err = a.client.Put("/bim/iam/bim/user/"+userid+"/profile", "", profile, &profile)
//	return
//}

func (r *BimUserResource) UpdateBimUserProfile(userid string, profile *BimUserProfile) (bimUserResponse *BimUser, err error) {
	err = r.client.Put("/bim/iam/bim/user/"+userid+"/profile", "", profile, &profile)
	return
}

// Domain specific types

type BimUserInput struct {
	Userid      string              `json:"userid"`
	Password    string              `json:"password"`
	Profile     BimUserProfileInput `json:"profile"`
	Permissions []interface{}       `json:"permissions,omitempty"`
}

type BimUser struct {
	Userid  string         `json:"userid"`
	Iamid   string         `json:"iamid"`
	Profile BimUserProfile `json:"profile"`
}

type BimUserProfileInput struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

type BimUserProfile struct {
	BimUserProfileInput
	ExternalUserIds struct {
		SnowflakeUser string `json:"snowflakeUser,omitempty"`
	} `json:"externalUserIds,omitempty"`
}

type BimUserCreateResponse struct {
	NewUser struct {
		BimUser
	} `json:"newUser"`
}

type BimUsers struct {
	Users []BimUser `json:"users"`
	Count int       `json:"count"`
}
