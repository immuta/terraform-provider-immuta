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
	_ resource.Resource                = (*groupAttributeResource)(nil)
	_ resource.ResourceWithConfigure   = (*groupAttributeResource)(nil)
	_ resource.ResourceWithImportState = (*groupAttributeResource)(nil)
)

// NewGroupAttributeResource returns the immuta_group_attribute resource.
func NewGroupAttributeResource() resource.Resource {
	return &groupAttributeResource{}
}

type groupAttributeResource struct {
	client *immutaapi.Client
}

// groupAttributeResourceModel is the Terraform representation of a single
// attribute value held against a group.
type groupAttributeResourceModel struct {
	ID      types.String `tfsdk:"id"`
	IAMID   types.String `tfsdk:"iam_id"`
	GroupID types.Int64  `tfsdk:"group_id"`
	Name    types.String `tfsdk:"name"`
	Value   types.String `tfsdk:"value"`
}

func (r *groupAttributeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group_attribute"
}

func (r *groupAttributeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Assigns one attribute value to an Immuta group with the documented " +
			"`/bim/iam/{iamid}/group/{groupId}/authorizations/{name}/{value}` API.\n\n" +
			"Members of the group inherit the attribute. One resource holds one name and value pair, so attributes assigned " +
			"outside Terraform are left alone.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the assignment, in the form `{iam_id}/{group_id}/{name}/{value}`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"iam_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(immutaapi.IAMIDBIM),
				MarkdownDescription: fmt.Sprintf("Identity manager the group belongs to. Defaults to `%s`. Changing it replaces the assignment.", immutaapi.IAMIDBIM),
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"group_id": schema.Int64Attribute{
				Required:            true,
				MarkdownDescription: "Numeric ID of the group, for example `immuta_group.engineering.id`. Changing it replaces the assignment.",
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

func (r *groupAttributeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *groupAttributeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan groupAttributeResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	iamID := plan.IAMID.ValueString()
	groupID := plan.GroupID.ValueInt64()
	name := plan.Name.ValueString()
	value := plan.Value.ValueString()

	modelID := strconv.FormatInt(groupID, 10)

	if err := r.client.SetAttribute(ctx, iamID, immutaapi.ModelTypeGroup, modelID, name, value); err != nil {
		resp.Diagnostics.Append(attributeErrorDiagnostic(
			fmt.Sprintf("Unable to assign the attribute %s to the Immuta group %d", name, groupID),
			err,
		))

		return
	}

	tflog.Debug(ctx, "Assigned an attribute to an Immuta group", map[string]any{"group_id": groupID, "name": name})

	plan.ID = types.StringValue(attributeID(iamID, modelID, name, value))

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupAttributeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state groupAttributeResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	iamID := firstNonEmpty(state.IAMID.ValueString(), immutaapi.IAMIDBIM)
	groupID := state.GroupID.ValueInt64()
	name := state.Name.ValueString()
	value := state.Value.ValueString()

	group, err := r.client.GetGroup(ctx, groupID)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			tflog.Debug(ctx, "Immuta group no longer exists, removing its attribute from state", map[string]any{"group_id": groupID})
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.Append(attributeErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta group %d", groupID), err))

		return
	}

	if !group.Authorizations.Has(name, value) {
		tflog.Debug(ctx, "Immuta group attribute no longer assigned, removing it from state", map[string]any{
			"group_id": groupID,
			"name":     name,
		})
		resp.State.RemoveResource(ctx)

		return
	}

	state.IAMID = types.StringValue(firstNonEmpty(group.IAMID, iamID))
	state.ID = types.StringValue(attributeID(state.IAMID.ValueString(), strconv.FormatInt(groupID, 10), name, value))

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update exists to satisfy the resource interface. Every configurable
// attribute requires replacement, so there is nothing to send.
func (r *groupAttributeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state groupAttributeResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = state.ID

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupAttributeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state groupAttributeResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	iamID := firstNonEmpty(state.IAMID.ValueString(), immutaapi.IAMIDBIM)
	groupID := state.GroupID.ValueInt64()
	name := state.Name.ValueString()

	err := r.client.DeleteAttribute(ctx, iamID, immutaapi.ModelTypeGroup, strconv.FormatInt(groupID, 10), name, state.Value.ValueString())
	if err != nil {
		if immutaapi.IsNotFound(err) {
			// Already gone remotely, nothing left to do.
			return
		}

		resp.Diagnostics.Append(attributeErrorDiagnostic(
			fmt.Sprintf("Unable to remove the attribute %s from the Immuta group %d", name, groupID),
			err,
		))
	}
}

// ImportState accepts "{iam_id}/{group_id}/{name}/{value}", for example
// "bim/12/Department/Finance". Attribute names and values containing a slash
// cannot be imported.
func (r *groupAttributeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(strings.TrimSpace(req.ID), "/")
	if len(parts) != 4 || parts[0] == "" || parts[2] == "" || parts[3] == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID for an Immuta group attribute",
			fmt.Sprintf("Import a group attribute as \"{iam_id}/{group_id}/{name}/{value}\", for example \"bim/12/Department/Finance\". Got %q.", req.ID),
		)

		return
	}

	groupID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid import ID for an Immuta group attribute",
			fmt.Sprintf("The group ID must be numeric, got %q.", parts[1]),
		)

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("iam_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("group_id"), groupID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[2])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("value"), parts[3])...)
}
