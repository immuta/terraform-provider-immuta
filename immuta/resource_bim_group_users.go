package immuta

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/instacart/terraform-provider-immuta/client"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &BimGroupUsersResource{}
var _ resource.ResourceWithImportState = &BimGroupUsersResource{}

func NewBimGroupUsersResource() resource.Resource {
	return &BimGroupUsersResource{}
}

// BimGroupUserResource defines the resource implementation.
type BimGroupUsersResource struct {
	client *client.ImmutaClient
}

type BimGroupUsersResourceModel struct {
	// https://documentation.immuta.com/SaaS/policy-as-code/v1-api/configure/bim/#remove-a-user-or-groups-attribute
	Id    types.Number `tfsdk:"id"`
	Users types.List   `tfsdk:"users"`
}

type UserAttribute struct {
	Group   types.Number `tfsdk:"group"`
	Id      types.Number `tfsdk:"id"`
	UserId  types.String `tfsdk:"userid"`
	IamId   types.String `tfsdk:"iamid"`
	Profile types.Number `tfsdk:"profile"`
}

func (r *BimGroupUsersResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bim_group_users"
}

func (r *BimGroupUsersResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "A resource to record all users within a group.",

		Attributes: map[string]schema.Attribute{
			"id": numberResourceId(), // we use the group id as the resource id
			"users": schema.ListNestedAttribute{
				MarkdownDescription: "The list of users within the group.",
				Required:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"group": schema.NumberAttribute{
							Required:    true,
							Description: "Group Id",
						},
						"id": numberResourceId(),
						"userid": schema.StringAttribute{
							Required:    true,
							Description: "The user's ID",
						},
						"iamid": schema.StringAttribute{
							Required:    true,
							Description: "The IamID",
						},
						"profile": schema.NumberAttribute{
							Required:    true,
							Description: "The user's profile Id",
						},
					},
				},
			},
		},
	}
}

func (r *BimGroupUsersResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *BimGroupUsersResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *BimGroupUsersResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	readingFailedErrorMessage := "Error reading BimGroupUsers"

	doesExist, err := r.ConfirmGroupExists(data.Id.String()) // we need to make sure the group does exist
	if err != nil {
		resp.Diagnostics.AddError(readingFailedErrorMessage, err.Error())
		return
	}
	if !doesExist {
		resp.State.RemoveResource(ctx)
		resp.Diagnostics.AddError(
			readingFailedErrorMessage,
			fmt.Sprintf("The given group [%s] does not exist. %s", data.Id.String(), err),
		)
		return
	}

	bimGroupUsersResponse, err := r.GetBimGroupUsers(data.Id.String())
	if err != nil {
		resp.Diagnostics.AddError(
			readingFailedErrorMessage,
			fmt.Sprintf("Error reading BimGroupUsers: %s", err),
		)
		return
	}
	newUsers := make([]UserAttribute, bimGroupUsersResponse.Count)
	if bimGroupUsersResponse.Count != 0 {
		for i, bimGroupUser := range bimGroupUsersResponse.Hits {
			user := BimGroupUserToUserAttribute(bimGroupUser)
			if user.Group.String() != data.Id.String() {
				resp.Diagnostics.AddError(
					readingFailedErrorMessage,
					fmt.Sprintf("Error reading BimGroupUsers, group ID has changed, old:[%s] new:[%s]: %s", data.Id.String(), user.Group.String(), err),
				)
				return
			}
			newUsers[i] = user
		}
	}
	usersList, convertDiags := UserAttributeListFromGo(ctx, newUsers)
	if convertDiags != nil {
		resp.Diagnostics.AddError(
			readingFailedErrorMessage,
			fmt.Sprintf("Error coverting users back to TF list: %s", convertDiags),
		)
		return
	}
	data.Users = usersList

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BimGroupUsersResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *BimGroupUsersResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	creatingFailedErrorMessage := "Error creating BimGroupUsers"

	if data.Users.Elements() != nil && len(data.Users.Elements()) > 0 {
		var users []UserAttribute
		if diags := data.Users.ElementsAs(ctx, &users, false); diags != nil && diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}
		groupId := (users[0].Group.String())

		// Validate the group exist and all users are ading to the same group
		doesExist, err := r.ConfirmGroupExists(groupId)
		if !doesExist {
			if err != nil {
				resp.Diagnostics.AddError(creatingFailedErrorMessage, fmt.Sprintf("Error reading the group with ID[%s]. %s", groupId, err))
			} else {
				resp.Diagnostics.AddError(creatingFailedErrorMessage, fmt.Sprintf("Cannot find the group with ID[%s].", groupId))
			}
			return
		}
		for _, user := range users {
			if user.Group.String() != groupId {
				resp.Diagnostics.AddError(creatingFailedErrorMessage, "All users need to have same target group ID.")
				return
			}
		}

		// add all users to the group
		for idx, user := range users {
			userInput := UserInput{}
			userInput.UserId = user.UserId.ValueString()
			userInput.IamId = user.IamId.ValueString()
			groupUserResponse, err := r.AddUserToGroup(user.Group.String(), userInput)
			if err != nil {
				resp.Diagnostics.AddError(creatingFailedErrorMessage, fmt.Sprintf("Error add a user to gorup: %s", err))
				return
			}
			users[idx].Id = intToNumberValue(groupUserResponse.Id)
			users[idx].Profile = intToNumberValue(groupUserResponse.Profile)
		}
		usersList, convertDiags := UserAttributeListFromGo(ctx, users)
		if convertDiags != nil {
			resp.Diagnostics.AddError(
				creatingFailedErrorMessage,
				fmt.Sprintf("Error coverting users back to TF list: %s", convertDiags),
			)
			return
		}
		data.Users = usersList
		data.Id = users[0].Group
	} else {
		resp.Diagnostics.AddError(creatingFailedErrorMessage, "Users list is empty")
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BimGroupUsersResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *BimGroupUsersResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if data.Users.Elements() != nil && len(data.Users.Elements()) > 0 {
		users := make([]UserAttribute, 0)
		if diags := data.Users.ElementsAs(ctx, &users, false); diags != nil && diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}
		for _, user := range users {
			err := r.RemoveUserFromGroup(user.Group.String(), user.Id.String())
			if err != nil {
				resp.Diagnostics.AddError(
					"Error deleting BimGroupUsers.",
					fmt.Sprintf("Error removing user [%s] from the group [%s]: %s", user.UserId.String(), user.Group.String(), err),
				)
				return
			}
		}
	}
}

// Update group details
func (r *BimGroupUsersResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *BimGroupUsersResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	updatingFailedErrorMessage := "Error updating BimGroupUsers"

	groupId := data.Id.String()

	var newUsers []UserAttribute
	if diags := data.Users.ElementsAs(ctx, &newUsers, false); diags != nil && diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	newUsersMap := make(map[string]UserAttribute)
	for _, newUser := range newUsers {
		newUsersMap[newUser.UserId.ValueString()] = newUser
	}
	existingUsersMap := make(map[string]UserAttribute)

	bimGroupUsersResponse, err := r.GetBimGroupUsers(groupId) // Fetch current users list
	if err != nil {
		resp.Diagnostics.AddError(
			updatingFailedErrorMessage,
			fmt.Sprintf("Error reading existing BimGroupUsers: %s", err),
		)
		return
	}

	// remove existing users if they are not in the newUsersMap
	if bimGroupUsersResponse.Count != 0 {
		for _, bimGroupUser := range bimGroupUsersResponse.Hits {
			existingUser := BimGroupUserToUserAttribute(bimGroupUser)
			if _, ok := newUsersMap[bimGroupUser.UserId]; ok {
				existingUsersMap[bimGroupUser.UserId] = existingUser
			} else {
				err := r.RemoveUserFromGroup(existingUser.Group.String(), existingUser.Id.String())
				if err != nil {
					resp.Diagnostics.AddError(
						updatingFailedErrorMessage,
						fmt.Sprintf("Error removing the user [%s] from the group [%d]: %s", bimGroupUser.UserId, bimGroupUser.Group, err),
					)
					return
				}
			}
		}
	}
	// add users to the group if they are not in existingUsersMap
	if len(newUsers) != 0 {
		for idx, newUser := range newUsers {
			if existingUser, ok := existingUsersMap[newUser.UserId.ValueString()]; ok {
				if newUser.Group.String() != existingUser.Group.String() || newUser.IamId.ValueString() != existingUser.IamId.ValueString() {
					resp.Diagnostics.AddError(
						updatingFailedErrorMessage,
						fmt.Sprintf("The user's group (old [%s], new [%s]) or iamid (old [%s], new [%s]) are not allowed to change.",
							existingUser.Group.String(), newUser.Group.String(), existingUser.IamId.ValueString(), newUser.IamId.ValueString()),
					)
					return
				}
			} else {
				if newUser.Group.String() != groupId {
					resp.Diagnostics.AddError(
						updatingFailedErrorMessage,
						fmt.Sprintf("The group id [%s] of the user should be the same as existing group id [%s]: %s", newUser.Group.String(), groupId, err),
					)
					return
				}
				userInput := UserInput{}
				userInput.UserId = newUser.UserId.ValueString()
				userInput.IamId = newUser.IamId.ValueString()
				groupUserResponse, err := r.AddUserToGroup(groupId, userInput)
				if err != nil {
					resp.Diagnostics.AddError(updatingFailedErrorMessage, fmt.Sprintf("Error add a user to gorup: %s", err))
					return
				}
				newUsers[idx].Id = intToNumberValue(groupUserResponse.Id)
				newUsers[idx].Profile = intToNumberValue(groupUserResponse.Profile)
			}
		}
		newUsersList, convertDiags := UserAttributeListFromGo(ctx, newUsers)

		if convertDiags != nil {
			resp.Diagnostics.AddError(
				updatingFailedErrorMessage,
				fmt.Sprintf("Error coverting users back to TF list: %s", convertDiags),
			)
			return
		}
		data.Users = newUsersList
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BimGroupUsersResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// CRUD methods

func (r *BimGroupUsersResource) GetBimGroupUsers(groupId string) (bimGroupUsersResponse *BimGroupUsers, err error) {
	err = r.client.Get(fmt.Sprintf("/bim/group/%s/user", groupId), "", nil, &bimGroupUsersResponse)
	return
}

func (r *BimGroupUsersResource) AddUserToGroup(groupId string, userInput UserInput) (groupUserResponse *GroupUserResponse, err error) {
	err = r.client.Post(fmt.Sprintf("/bim/group/%s/user", groupId), "", userInput, &groupUserResponse)
	return
}

func (r *BimGroupUsersResource) RemoveUserFromGroup(groupId string, groupUserId string) (err error) {
	err = r.client.Delete(fmt.Sprintf("/bim/group/%s/user/%s", groupId, groupUserId), "", nil, nil)
	return
}

// helper methods

func (r *BimGroupUsersResource) ConfirmGroupExists(groupId string) (doesExist bool, err error) {
	err = r.client.Get(fmt.Sprintf("/bim/group/%s", groupId), "", nil, nil)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func UserAttributeListFromGo(ctx context.Context, users []UserAttribute) (types.List, diag.Diagnostics) {
	userTypes := map[string]attr.Type{
		"group":   types.NumberType,
		"id":      types.NumberType,
		"userid":  types.StringType,
		"iamid":   types.StringType,
		"profile": types.NumberType,
	}
	usersList, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: userTypes}, users)
	return usersList, diags
}

func BimGroupUserToUserAttribute(bimGroupUser BimGroupUser) UserAttribute {
	user := UserAttribute{}
	user.Group = intToNumberValue(bimGroupUser.Group)
	user.Id = intToNumberValue(bimGroupUser.Id)
	user.UserId = types.StringValue(bimGroupUser.UserId)
	user.IamId = types.StringValue(bimGroupUser.IamId)
	user.Profile = intToNumberValue(bimGroupUser.Profile.Id)
	return user
}

// Domain-specific methods

type BimGroupUserProfile struct {
	Id              int         `json:"id"`
	Name            string      `json:"name"`
	Email           string      `json:"email"`
	Phone           string      `json:"phone"`
	About           string      `json:"about"`
	Location        string      `json:"location"`
	Organization    string      `json:"organization"`
	Position        string      `json:"position"`
	Preferences     interface{} `json:"preferences"`
	ExternalUserIds interface{} `json:"externalUserIds"`
	Scim            string      `json:"scim"`
	SystemGenerated bool        `json:"systemGenerated"`
	CreatedAt       time.Time   `json:"createdAt"`
	UpdatedAt       time.Time   `json:"updatedAt"`
}

type BimGroupUser struct {
	Id        int                 `json:"id"`
	Group     int                 `json:"group"`
	Profile   BimGroupUserProfile `json:"profile"`
	UserId    string              `json:"userid"`
	Uid       int                 `json:"uid"`
	IamId     string              `json:"iamid"`
	Disabled  bool                `json:"disabled"`
	CreatedAt time.Time           `json:"createdAt"`
	UpdatedAt time.Time           `json:"updatedAt"`
}

type BimGroupUsers struct {
	Count int            `json:"count"`
	Hits  []BimGroupUser `json:"hits"`
}

type UserInput struct {
	UserId string `json:"userid"`
	IamId  string `json:"iamid"`
}

type GroupUserResponse struct {
	Id        int       `json:"id"`
	Group     int       `json:"group"`
	Profile   int       `json:"profile"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
