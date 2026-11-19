package provider

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ resource.Resource                = (*dataObjectTagResource)(nil)
	_ resource.ResourceWithConfigure   = (*dataObjectTagResource)(nil)
	_ resource.ResourceWithImportState = (*dataObjectTagResource)(nil)
)

// NewDataObjectTagResource returns the immuta_data_object_tag resource.
func NewDataObjectTagResource() resource.Resource {
	return &dataObjectTagResource{}
}

type dataObjectTagResource struct {
	client *immutaapi.Client
}

type dataObjectTagResourceModel struct {
	ID            types.String `tfsdk:"id"`
	ConnectionKey types.String `tfsdk:"connection_key"`
	DataObjectID  types.String `tfsdk:"data_object_id"`
	TagID         types.String `tfsdk:"tag_id"`
	TagName       types.String `tfsdk:"tag_name"`
}

func (r *dataObjectTagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_object_tag"
}

func (r *dataObjectTagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Applies one tag to a data object (a catalog, database, schema or table) that a connection " +
			"registered, with the documented `tags` field of the connections API data object update. Each resource " +
			"manages a single tag on a single data object; every other tag on the data object is left as it is.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`<connection_key>/<data_object_id>/<tag_id>`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"connection_key": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Key of the connection that holds the data object, usually " +
					"`data.immuta_data_object.<name>.connection_key`. Changing it replaces the resource.",
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: replace,
			},
			"data_object_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "ID of the data object to tag, usually `data.immuta_data_object.<name>.id`. Changing " +
					"it replaces the resource.",
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: replace,
			},
			"tag_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Numeric ID of the tag to apply, usually `immuta_tag.<name>.id` or " +
					"`data.immuta_tag.<name>.id`. Changing it replaces the resource.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^[0-9]+$`), "must be a numeric tag ID"),
				},
				PlanModifiers: replace,
			},
			"tag_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Fully qualified name of the tag, as Immuta reports it on the data object.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *dataObjectTagResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *dataObjectTagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dataObjectTagResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, objectID := plan.ConnectionKey.ValueString(), plan.DataObjectID.ValueString()
	tagID, diags := parseTagID(plan.TagID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := dataObjectTagID(key, objectID, tagID)

	// A tag already applied directly is not adopted silently, since destroying
	// this resource would then remove a tag Terraform did not apply.
	object, err := r.client.GetDataObject(ctx, key, objectID)
	if err != nil {
		resp.Diagnostics.Append(connectionErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta data object %s", objectID), err))

		return
	}
	if tag, ok := object.DirectTag(tagID); ok {
		resp.Diagnostics.AddError("Tag already applied to the Immuta data object",
			fmt.Sprintf("The tag %s (%d) is already applied to the data object %s. Import it to manage it with "+
				"Terraform:\n\n  terraform import <resource address> %s", tag.Name, tagID, objectID, id))

		return
	}

	if err := r.client.PatchDataObjectTags(ctx, key, objectID, []int64{tagID}, nil); err != nil {
		resp.Diagnostics.Append(connectionErrorDiagnostic(fmt.Sprintf("Unable to apply the tag %d to the Immuta data object %s", tagID, objectID), err))

		return
	}

	tflog.Debug(ctx, "Applied a tag to an Immuta data object", map[string]any{"connection_key": key, "data_object_id": objectID, "tag_id": tagID})

	plan.ID = types.StringValue(id)
	found, diags := r.refresh(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError("The tag was not applied to the Immuta data object",
			fmt.Sprintf("Immuta accepted the request to apply the tag %d to the data object %s, but does not report it "+
				"on the data object afterwards.", tagID, objectID))

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dataObjectTagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dataObjectTagResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, diags := r.refresh(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		tflog.Debug(ctx, "Tag no longer applied to the Immuta data object, removing it from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called with a change: every configurable attribute
// requires replacement.
func (r *dataObjectTagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dataObjectTagResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes only the tag this resource applied. A data object or tag
// that is already gone needs nothing more.
func (r *dataObjectTagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dataObjectTagResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, objectID := state.ConnectionKey.ValueString(), state.DataObjectID.ValueString()
	tagID, diags := parseTagID(state.TagID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.PatchDataObjectTags(ctx, key, objectID, nil, []int64{tagID}); err != nil && !immutaapi.IsNotFound(err) {
		resp.Diagnostics.Append(connectionErrorDiagnostic(fmt.Sprintf("Unable to remove the tag %d from the Immuta data object %s", tagID, objectID), err))
	}
}

// ImportState imports a tag assignment by
// <connection_key>/<data_object_id>/<tag_id>.
func (r *dataObjectTagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Expected <connection_key>/<data_object_id>/<tag_id>, got %q.", req.ID))

		return
	}
	if _, err := strconv.ParseInt(parts[2], 10, 64); err != nil {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("The tag ID in %q must be numeric.", req.ID))

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("connection_key"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("data_object_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("tag_id"), parts[2])...)
}

// refresh reads the tag assignment into the model. It reports false when the
// data object no longer exists or the tag is no longer applied to it
// directly; a tag that is only inherited from a parent does not count.
func (r *dataObjectTagResource) refresh(ctx context.Context, model *dataObjectTagResourceModel) (bool, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	tagID, diags := parseTagID(model.TagID)
	diagnostics.Append(diags...)
	if diagnostics.HasError() {
		return true, diagnostics
	}

	key, objectID := model.ConnectionKey.ValueString(), model.DataObjectID.ValueString()
	object, err := r.client.GetDataObject(ctx, key, objectID)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			return false, diagnostics
		}
		diagnostics.Append(connectionErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta data object %s", objectID), err))

		return true, diagnostics
	}

	tag, ok := object.DirectTag(tagID)
	if !ok {
		return false, diagnostics
	}

	model.ID = types.StringValue(dataObjectTagID(key, objectID, tagID))
	model.TagName = types.StringValue(tag.Name)

	return true, diagnostics
}

func parseTagID(value types.String) (int64, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	id, err := strconv.ParseInt(value.ValueString(), 10, 64)
	if err != nil || id < 0 {
		diagnostics.AddAttributeError(path.Root("tag_id"), "Invalid tag ID",
			fmt.Sprintf("The tag ID must be a non-negative number, got %q.", value.ValueString()))
	}

	return id, diagnostics
}

func dataObjectTagID(connectionKey, dataObjectID string, tagID int64) string {
	return fmt.Sprintf("%s/%s/%d", connectionKey, dataObjectID, tagID)
}
