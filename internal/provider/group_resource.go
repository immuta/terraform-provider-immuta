package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ resource.Resource                = (*groupResource)(nil)
	_ resource.ResourceWithConfigure   = (*groupResource)(nil)
	_ resource.ResourceWithImportState = (*groupResource)(nil)
)

// NewGroupResource returns the immuta_group resource.
func NewGroupResource() resource.Resource {
	return &groupResource{}
}

type groupResource struct {
	client *immutaapi.Client
}

// groupResourceModel is the Terraform representation of a group.
type groupResourceModel struct {
	ID          types.String `tfsdk:"id"`
	IAMID       types.String `tfsdk:"iam_id"`
	Name        types.String `tfsdk:"name"`
	Email       types.String `tfsdk:"email"`
	Description types.String `tfsdk:"description"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func (r *groupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

func (r *groupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an Immuta group with the documented `/bim/group` API.\n\n" +
			"Group membership is managed with `immuta_group_user` and group attributes with `immuta_group_attribute`, so that " +
			"memberships and attributes owned by other configurations are not overwritten.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Numeric ID of the group, assigned by Immuta.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"iam_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(immutaapi.IAMIDBIM),
				MarkdownDescription: fmt.Sprintf("Identity manager the group belongs to. Defaults to `%s`, Immuta's built-in identity manager. Changing it replaces the group.", immutaapi.IAMIDBIM),
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the group. Changing the name updates the existing group in place.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"email": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Email address associated with the group.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the group that is displayed to users.",
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timestamp of when the group was created.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timestamp of when the group was last updated.",
			},
		},
	}
}

func (r *groupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *groupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan groupResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createRequest := immutaapi.CreateGroupRequest{
		IAMID:       plan.IAMID.ValueString(),
		Name:        plan.Name.ValueString(),
		Email:       optionalStringPointer(plan.Email),
		Description: optionalStringPointer(plan.Description),
	}

	created, err := r.client.CreateGroup(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.Append(groupErrorDiagnostic("Unable to create the Immuta group", err))

		return
	}

	tflog.Debug(ctx, "Created an Immuta group", map[string]any{"id": created.ID})

	// Refresh after create so that state matches what the API stored.
	group, err := r.client.GetGroup(ctx, created.ID)
	if err != nil {
		resp.Diagnostics.Append(groupErrorDiagnostic(
			fmt.Sprintf("Unable to read the Immuta group %d back after creating it", created.ID),
			err,
		))

		return
	}

	plan.applyGroup(group)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state groupResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, ok := parseInt64Attribute(state.ID, path.Root("id"), "group", &resp.Diagnostics)
	if !ok {
		return
	}

	group, err := r.client.GetGroup(ctx, id)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			tflog.Debug(ctx, "Immuta group no longer exists, removing it from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.Append(groupErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta group %d", id), err))

		return
	}

	state.applyGroup(group)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *groupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state groupResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, ok := parseInt64Attribute(state.ID, path.Root("id"), "group", &resp.Diagnostics)
	if !ok {
		return
	}

	// Only send attributes the documented PUT endpoint can change, and only
	// when they actually changed.
	var updateRequest immutaapi.UpdateGroupRequest
	if !plan.Name.Equal(state.Name) {
		name := plan.Name.ValueString()
		updateRequest.Name = &name
	}
	if !plan.Email.Equal(state.Email) {
		email := plan.Email.ValueString()
		updateRequest.Email = &email
	}
	if !plan.Description.Equal(state.Description) {
		description := plan.Description.ValueString()
		updateRequest.Description = &description
	}

	if !updateRequest.IsEmpty() {
		if err := r.client.UpdateGroup(ctx, id, updateRequest); err != nil {
			resp.Diagnostics.Append(groupErrorDiagnostic(fmt.Sprintf("Unable to update the Immuta group %d", id), err))

			return
		}
	}

	// Refresh after update so that state matches what the API stored.
	group, err := r.client.GetGroup(ctx, id)
	if err != nil {
		resp.Diagnostics.Append(groupErrorDiagnostic(
			fmt.Sprintf("Unable to read the Immuta group %d back after updating it", id),
			err,
		))

		return
	}

	plan.ID = state.ID
	plan.applyGroup(group)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state groupResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, ok := parseInt64Attribute(state.ID, path.Root("id"), "group", &resp.Diagnostics)
	if !ok {
		return
	}

	if err := r.client.DeleteGroup(ctx, id); err != nil {
		if immutaapi.IsNotFound(err) {
			// Already gone remotely, nothing left to do.
			return
		}

		resp.Diagnostics.Append(groupErrorDiagnostic(fmt.Sprintf("Unable to delete the Immuta group %d", id), err))
	}
}

func (r *groupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// applyGroup copies an API group onto the Terraform model.
func (m *groupResourceModel) applyGroup(group *immutaapi.Group) {
	m.ID = types.StringValue(strconv.FormatInt(group.ID, 10))
	m.IAMID = knownStringOr(group.IAMID, m.IAMID)
	m.Name = types.StringValue(group.Name)
	m.Email = normalizeOptionalString(group.Email, m.Email)
	m.Description = normalizeOptionalString(group.Description, m.Description)
	m.CreatedAt = optionalComputedString(group.CreatedAt)
	m.UpdatedAt = optionalComputedString(group.UpdatedAt)
}
