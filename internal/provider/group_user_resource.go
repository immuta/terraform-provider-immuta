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
	_ resource.Resource                = (*groupUserResource)(nil)
	_ resource.ResourceWithConfigure   = (*groupUserResource)(nil)
	_ resource.ResourceWithImportState = (*groupUserResource)(nil)
)

// NewGroupUserResource returns the immuta_group_user resource.
func NewGroupUserResource() resource.Resource {
	return &groupUserResource{}
}

type groupUserResource struct {
	client *immutaapi.Client
}

// groupUserResourceModel is the Terraform representation of one user's
// membership of one group.
type groupUserResourceModel struct {
	ID           types.String `tfsdk:"id"`
	IAMID        types.String `tfsdk:"iam_id"`
	GroupID      types.Int64  `tfsdk:"group_id"`
	UserID       types.String `tfsdk:"user_id"`
	MembershipID types.Int64  `tfsdk:"membership_id"`
}

func (r *groupUserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group_user"
}

func (r *groupUserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Adds one user to one Immuta group with the documented `/bim/group/{groupId}/user` API.\n\n" +
			"Each membership is a separate resource, so memberships created outside Terraform or by another configuration are left alone. " +
			"Destroying the resource removes the user from the group; it does not delete the user or the group.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the membership, in the form `{iam_id}/{user_id}/{group_id}`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"iam_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(immutaapi.IAMIDBIM),
				MarkdownDescription: fmt.Sprintf("Identity manager the user belongs to. Defaults to `%s`. Changing it replaces the membership.", immutaapi.IAMIDBIM),
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"group_id": schema.Int64Attribute{
				Required:            true,
				MarkdownDescription: "Numeric ID of the group, for example `immuta_group.engineering.id`. Changing it replaces the membership.",
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"user_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Username of the user, for example `immuta_user.jane.user_id`. Changing it replaces the membership.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"membership_id": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Numeric ID Immuta assigns to the membership itself, used to remove the user from the group.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *groupUserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *groupUserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan groupUserResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	iamID := plan.IAMID.ValueString()
	userID := plan.UserID.ValueString()
	groupID := plan.GroupID.ValueInt64()

	membership, err := r.client.AddGroupUser(ctx, groupID, iamID, userID)
	if err != nil {
		resp.Diagnostics.Append(membershipErrorDiagnostic(
			fmt.Sprintf("Unable to add the Immuta user %s to the group %d", userID, groupID),
			err,
		))

		return
	}

	tflog.Debug(ctx, "Added an Immuta user to a group", map[string]any{
		"group_id":      groupID,
		"membership_id": membership.ID,
	})

	plan.MembershipID = types.Int64Value(membership.ID)
	plan.ID = types.StringValue(groupUserID(iamID, userID, groupID))

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupUserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state groupUserResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	iamID := firstNonEmpty(state.IAMID.ValueString(), immutaapi.IAMIDBIM)
	userID := state.UserID.ValueString()
	groupID := state.GroupID.ValueInt64()

	// The group membership list is the only documented way to recover the
	// membership id, which is what removing the user from the group needs.
	membership, err := r.client.FindUserGroup(ctx, iamID, userID, groupID)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			tflog.Debug(ctx, "Immuta group membership no longer exists, removing it from state", map[string]any{
				"group_id": groupID,
				"user_id":  userID,
			})
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.Append(membershipErrorDiagnostic(
			fmt.Sprintf("Unable to read the groups of the Immuta user %s", userID),
			err,
		))

		return
	}

	state.IAMID = types.StringValue(iamID)
	state.MembershipID = types.Int64Value(membership.GroupUser)
	state.ID = types.StringValue(groupUserID(iamID, userID, groupID))

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update exists to satisfy the resource interface. Every configurable
// attribute requires replacement, so Terraform never calls it with a change
// the API could apply.
func (r *groupUserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state groupUserResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = state.ID
	plan.MembershipID = state.MembershipID

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupUserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state groupUserResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupID := state.GroupID.ValueInt64()
	membershipID := state.MembershipID.ValueInt64()

	if err := r.client.RemoveGroupUser(ctx, groupID, membershipID); err != nil {
		if immutaapi.IsNotFound(err) {
			// Already gone remotely, nothing left to do.
			return
		}

		resp.Diagnostics.Append(membershipErrorDiagnostic(
			fmt.Sprintf("Unable to remove the Immuta user %s from the group %d", state.UserID.ValueString(), groupID),
			err,
		))
	}
}

// ImportState accepts "{iam_id}/{user_id}/{group_id}", for example
// "bim/jane.doe@immuta.com/12".
func (r *groupUserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	iamID, userID, rawGroupID, ok := splitGroupUserID(req.ID)
	if !ok {
		resp.Diagnostics.AddError(
			"Invalid import ID for an Immuta group membership",
			fmt.Sprintf("Import a membership as \"{iam_id}/{user_id}/{group_id}\", for example \"bim/jane.doe@immuta.com/12\". Got %q.", req.ID),
		)

		return
	}

	groupID, err := strconv.ParseInt(rawGroupID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid import ID for an Immuta group membership",
			fmt.Sprintf("The group ID must be numeric, got %q.", rawGroupID),
		)

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("iam_id"), iamID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), userID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("group_id"), groupID)...)
}

func groupUserID(iamID, userID string, groupID int64) string {
	return fmt.Sprintf("%s/%s/%d", iamID, userID, groupID)
}

// splitGroupUserID splits "{iam_id}/{user_id}/{group_id}". The IAM id is
// taken from the front and the group id from the back so that a username
// containing a slash still round-trips.
func splitGroupUserID(id string) (iamID, userID, groupID string, ok bool) {
	parts := strings.Split(strings.TrimSpace(id), "/")
	if len(parts) < 3 {
		return "", "", "", false
	}

	iamID = parts[0]
	groupID = parts[len(parts)-1]
	userID = strings.Join(parts[1:len(parts)-1], "/")

	if iamID == "" || userID == "" || groupID == "" {
		return "", "", "", false
	}

	return iamID, userID, groupID, true
}
