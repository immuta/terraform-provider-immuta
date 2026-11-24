package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ resource.Resource                = (*userAttributeResource)(nil)
	_ resource.ResourceWithConfigure   = (*userAttributeResource)(nil)
	_ resource.ResourceWithImportState = (*userAttributeResource)(nil)
)

// NewUserAttributeResource returns the immuta_user_attribute resource.
func NewUserAttributeResource() resource.Resource {
	return &userAttributeResource{}
}

type userAttributeResource struct {
	client *immutaapi.Client
}

// userAttributeResourceModel is the Terraform representation of a single
// attribute value held against a user.
type userAttributeResourceModel struct {
	ID            types.String `tfsdk:"id"`
	IAMID         types.String `tfsdk:"iam_id"`
	UserID        types.String `tfsdk:"user_id"`
	UserProfileID types.Int64  `tfsdk:"user_profile_id"`
	Name          types.String `tfsdk:"name"`
	Value         types.String `tfsdk:"value"`
}

func (r *userAttributeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user_attribute"
}

func (r *userAttributeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Assigns one attribute value to an Immuta user with the documented " +
			"`/bim/iam/{iamid}/user/{userid}/authorizations/{name}/{value}` API.\n\n" +
			"One resource holds one name and value pair. Assign several resources to give a user several values for the same " +
			"attribute. Attributes assigned outside Terraform are left alone.\n\n" +
			"Immuta identifies a user by username on the write endpoints and by numeric profile ID on the read endpoint, so both " +
			"are required. Take them from the same `immuta_user` resource to keep them in step.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the assignment, in the form `{iam_id}/{user_profile_id}/{user_id}/{name}/{value}`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"iam_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(immutaapi.IAMIDBIM),
				MarkdownDescription: fmt.Sprintf("Identity manager the user belongs to. Defaults to `%s`. Changing it replaces the assignment.", immutaapi.IAMIDBIM),
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"user_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Username of the user, for example `immuta_user.jane.user_id`. Changing it replaces the assignment.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"user_profile_id": schema.Int64Attribute{
				Required:            true,
				MarkdownDescription: "Numeric profile ID of the same user, for example `immuta_user.jane.id`. Used to read the assignment back. Changing it replaces the assignment.",
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the attribute, for example `Department`. Changing it replaces the assignment.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"value": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Value of the attribute, for example `Finance`. Changing it replaces the assignment.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *userAttributeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *userAttributeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userAttributeResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	iamID := plan.IAMID.ValueString()
	userID := plan.UserID.ValueString()
	name := plan.Name.ValueString()
	value := plan.Value.ValueString()

	err := r.client.SetAttribute(ctx, iamID, immutaapi.ModelTypeUser, userID, name, value)
	if err != nil {
		resp.Diagnostics.Append(attributeErrorDiagnostic(
			fmt.Sprintf("Unable to assign the attribute %s to the Immuta user %s", name, userID),
			err,
		))

		return
	}

	tflog.Debug(ctx, "Assigned an attribute to an Immuta user", map[string]any{"user_id": userID, "name": name})

	plan.ID = types.StringValue(userAttributeID(iamID, plan.UserProfileID.ValueInt64(), userID, name, value))

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userAttributeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userAttributeResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	iamID := firstNonEmpty(state.IAMID.ValueString(), immutaapi.IAMIDBIM)
	profileID := state.UserProfileID.ValueInt64()
	name := state.Name.ValueString()
	value := state.Value.ValueString()

	user, err := r.client.GetUser(ctx, iamID, profileID)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			tflog.Debug(ctx, "Immuta user no longer exists, removing its attribute from state", map[string]any{"user_profile_id": profileID})
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.Append(attributeErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta user %d", profileID), err))

		return
	}

	if !userHoldsAttribute(user, name, value) {
		tflog.Debug(ctx, "Immuta user attribute no longer assigned, removing it from state", map[string]any{
			"user_profile_id": profileID,
			"name":            name,
		})
		resp.State.RemoveResource(ctx)

		return
	}

	state.IAMID = types.StringValue(iamID)
	state.UserID = knownStringOr(user.UserID, state.UserID)
	state.ID = types.StringValue(userAttributeID(iamID, profileID, state.UserID.ValueString(), name, value))

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update exists to satisfy the resource interface. Every configurable
// attribute requires replacement, so there is nothing to send.
func (r *userAttributeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state userAttributeResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = state.ID

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userAttributeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userAttributeResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	iamID := firstNonEmpty(state.IAMID.ValueString(), immutaapi.IAMIDBIM)
	userID := state.UserID.ValueString()
	name := state.Name.ValueString()

	err := r.client.DeleteAttribute(ctx, iamID, immutaapi.ModelTypeUser, userID, name, state.Value.ValueString())
	if err != nil {
		if immutaapi.IsNotFound(err) {
			// Already gone remotely, nothing left to do.
			return
		}

		resp.Diagnostics.Append(attributeErrorDiagnostic(
			fmt.Sprintf("Unable to remove the attribute %s from the Immuta user %s", name, userID),
			err,
		))
	}
}

// userHoldsAttribute reports whether the attribute is assigned to the user's
// own BIM profile.
//
// bimAuthorizations holds the attributes given to the BIM profile, which is
// what this resource writes. authorizations is the user's combined view and
// also contains attributes inherited from their groups, so it is only
// consulted when Immuta did not return bimAuthorizations at all.
func userHoldsAttribute(user *immutaapi.User, name, value string) bool {
	if user.BIMAuthorizations != nil {
		return user.BIMAuthorizations.Has(name, value)
	}

	return user.Authorizations.Has(name, value)
}

// ImportState accepts
// "{iam_id}/{user_profile_id}/{user_id}/{name}/{value}", for example
// "bim/18/jane.doe@immuta.com/Department/Finance". Usernames, attribute names
// and values containing a slash cannot be imported.
func (r *userAttributeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(strings.TrimSpace(req.ID), "/")
	if len(parts) != 5 || parts[0] == "" || parts[2] == "" || parts[3] == "" || parts[4] == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID for an Immuta user attribute",
			fmt.Sprintf("Import a user attribute as \"{iam_id}/{user_profile_id}/{user_id}/{name}/{value}\", for example \"bim/18/jane.doe@immuta.com/Department/Finance\". Got %q.", req.ID),
		)

		return
	}

	profileID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid import ID for an Immuta user attribute",
			fmt.Sprintf("The user profile ID must be numeric, got %q.", parts[1]),
		)

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), userAttributeID(parts[0], profileID, parts[2], parts[3], parts[4]))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("iam_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_profile_id"), profileID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), parts[2])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[3])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("value"), parts[4])...)
}

// userAttributeID is also the import ID, so that importing a user
// attribute and reading it back produce the same identifier.
func userAttributeID(iamID string, profileID int64, userID, name, value string) string {
	return fmt.Sprintf("%s/%d/%s/%s/%s", iamID, profileID, userID, name, value)
}

func attributeID(iamID, modelID, name, value string) string {
	return fmt.Sprintf("%s/%s/%s/%s", iamID, modelID, name, value)
}
