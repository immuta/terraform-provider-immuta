package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

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
	_ resource.Resource                = (*customDataObjectResource)(nil)
	_ resource.ResourceWithConfigure   = (*customDataObjectResource)(nil)
	_ resource.ResourceWithImportState = (*customDataObjectResource)(nil)
)

// How long the provider waits for Immuta to finish renaming or deleting a
// Custom data object. A variable so that tests can shorten it.
var customDataObjectTaskTimeout = 10 * time.Minute

// NewCustomDataObjectResource returns the immuta_custom_data_object resource.
func NewCustomDataObjectResource() resource.Resource {
	return &customDataObjectResource{}
}

type customDataObjectResource struct {
	client *immutaapi.Client
}

type customDataObjectResourceModel struct {
	ID            types.String `tfsdk:"id"`
	ConnectionKey types.String `tfsdk:"connection_key"`
	Name          types.String `tfsdk:"name"`
	Type          types.String `tfsdk:"type"`
	ParentID      types.String `tfsdk:"parent_id"`
}

func (r *customDataObjectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_data_object"
}

func (r *customDataObjectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Creates a folder or a securable under a `Custom` connection, with the documented connections " +
			"API (`POST /api/connections/{connectionKey}/dataObjects`). A securable is what users get access to; bind an " +
			"identity group to it with `immuta_custom_securable_access_group`.\n\n" +
			"**Destroying or replacing a folder deletes it and everything under it in Immuta.**",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ID of the data object, the securable or folder ID used by other resources.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"connection_key": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Key of the `Custom` connection, usually `immuta_connection.<name>.connection_key`. " +
					"Changing it replaces the data object.",
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: replace,
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Name of the data object. It must be unique, for its type, under its parent. Changing " +
					"it renames the data object in place.",
				Validators: []validator.String{
					// Immuta trims names, so surrounding spaces would never match.
					stringvalidator.RegexMatches(regexp.MustCompile(`^\S(.*\S)?$`), "must not be empty or start or end with whitespace"),
				},
			},
			"type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: fmt.Sprintf("%s or %s. Changing it replaces the data object.",
					"`"+immutaapi.DataObjectTypeFolder+"`", "`"+immutaapi.DataObjectTypeSecurable+"`"),
				Validators: []validator.String{
					stringvalidator.OneOf(immutaapi.DataObjectTypeFolder, immutaapi.DataObjectTypeSecurable),
				},
				PlanModifiers: replace,
			},
			"parent_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "ID of the folder to create the data object in, usually " +
					"`immuta_custom_data_object.<folder>.id`. Leave it out to create the data object at the top of the " +
					"connection. Changing it replaces the data object.",
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: replace,
			},
		},
	}
}

func (r *customDataObjectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *customDataObjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan customDataObjectResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, name, objectType := plan.ConnectionKey.ValueString(), plan.Name.ValueString(), plan.Type.ValueString()

	object, err := r.client.CreateDataObject(ctx, key, name, objectType, plan.ParentID.ValueString())
	if err != nil {
		resp.Diagnostics.Append(customDataObjectErrorDiagnostic(
			fmt.Sprintf("Unable to create the %s %q in the Immuta connection %s", strings.ToLower(objectType), name, key), err))

		return
	}

	tflog.Debug(ctx, "Created an Immuta Custom data object", map[string]any{"connection_key": key, "id": object.ID, "type": objectType})

	plan.ID = types.StringValue(object.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *customDataObjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state customDataObjectResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, id := state.ConnectionKey.ValueString(), state.ID.ValueString()

	object, err := r.client.GetDataObject(ctx, key, id)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			tflog.Debug(ctx, "Immuta Custom data object no longer exists, removing it from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)

			return
		}
		resp.Diagnostics.Append(customDataObjectErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta data object %s", id), err))

		return
	}

	state.Name = types.StringValue(object.Name)
	state.Type = types.StringValue(object.Type)
	// A folder parent is read back, which also fills it in after an import.
	// A data object at the top of the connection keeps the configured value.
	if parent, ok := customDataObjectParentFolder(object); ok {
		state.ParentID = types.StringValue(parent)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update only renames: every other configurable attribute requires
// replacement.
func (r *customDataObjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state customDataObjectResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = state.ID
	key, id, name := state.ConnectionKey.ValueString(), state.ID.ValueString(), plan.Name.ValueString()

	if name != state.Name.ValueString() {
		if err := r.client.RenameDataObject(ctx, key, id, name); err != nil {
			resp.Diagnostics.Append(customDataObjectErrorDiagnostic(
				fmt.Sprintf("Unable to rename the Immuta data object %s to %q", id, name), err))

			return
		}
		resp.Diagnostics.Append(r.waitForName(ctx, key, id, name)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete deletes the data object and, for a folder, everything under it.
// Deletion is asynchronous; waiting for it means a replacement can create a
// data object with the same name straight away.
func (r *customDataObjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state customDataObjectResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, id := state.ConnectionKey.ValueString(), state.ID.ValueString()

	if err := r.client.DeleteDataObject(ctx, key, id); err != nil {
		if immutaapi.IsNotFound(err) {
			return
		}
		resp.Diagnostics.Append(customDataObjectErrorDiagnostic(fmt.Sprintf("Unable to delete the Immuta data object %s", id), err))

		return
	}

	deadline := time.Now().Add(customDataObjectTaskTimeout)
	for {
		_, err := r.client.GetDataObject(ctx, key, id)
		if immutaapi.IsNotFound(err) {
			return
		}
		if err != nil {
			resp.Diagnostics.Append(customDataObjectErrorDiagnostic(fmt.Sprintf("Unable to confirm the Immuta data object %s was deleted", id), err))

			return
		}
		if time.Now().After(deadline) {
			resp.Diagnostics.AddError(fmt.Sprintf("The Immuta data object %s is still being deleted", id),
				fmt.Sprintf("After %s the data object still exists. Check it in Immuta, then refresh.", customDataObjectTaskTimeout))

			return
		}
		if err := sleepContext(ctx, dataObjectPollInterval); err != nil {
			resp.Diagnostics.AddError("Stopped waiting for the Immuta data object to be deleted", err.Error())

			return
		}
	}
}

// ImportState imports a data object by <connection_key>/<data object id>.
func (r *customDataObjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	key, id, ok := strings.Cut(req.ID, "/")
	if !ok || key == "" || id == "" || strings.Contains(id, "/") {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Expected <connection_key>/<data object id>, got %q.", req.ID))

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("connection_key"), key)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// waitForName waits until Immuta reports the data object under its new
// name, since renames are asynchronous.
func (r *customDataObjectResource) waitForName(ctx context.Context, key, id, name string) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	deadline := time.Now().Add(customDataObjectTaskTimeout)
	for {
		object, err := r.client.GetDataObject(ctx, key, id)
		if err != nil {
			diagnostics.Append(customDataObjectErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta data object %s", id), err))

			return diagnostics
		}
		if object.Name == name {
			return diagnostics
		}
		if time.Now().After(deadline) {
			diagnostics.AddError(fmt.Sprintf("The Immuta data object %s is still being renamed", id),
				fmt.Sprintf("After %s Immuta still reports the name %q. Check it in Immuta, then refresh.", customDataObjectTaskTimeout, object.Name))

			return diagnostics
		}
		if err := sleepContext(ctx, dataObjectPollInterval); err != nil {
			diagnostics.AddError("Stopped waiting for the Immuta data object to be renamed", err.Error())

			return diagnostics
		}
	}
}

// customDataObjectParentFolder returns the ID of the folder that holds a
// data object, when its parent is a folder rather than the connection.
func customDataObjectParentFolder(object *immutaapi.DataObject) (string, bool) {
	if len(object.Path) < 2 {
		return "", false
	}

	parent := object.Path[len(object.Path)-2]
	if parent.Type != immutaapi.DataObjectTypeFolder || parent.ID == "" {
		return "", false
	}

	return parent.ID, true
}

// customDataObjectErrorDiagnostic explains the documented causes of a
// failed Custom data object or access group request.
func customDataObjectErrorDiagnostic(summary string, err error) diag.Diagnostic {
	detail := err.Error()

	var apiErr *immutaapi.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			detail += "\n\nCheck that the configured API key is valid and that the Immuta user holds the " +
				"APPLICATION_ADMIN or GOVERNANCE permission, or owns the data objects of the Custom connection."
		case http.StatusBadRequest:
			detail += "\n\nImmuta rejected the request; its reason is shown above. Folders and securables can only " +
				"be created in a Custom connection, at its top or in a folder."
		case http.StatusConflict:
			detail += "\n\nImmuta reported a conflict. A data object with this name and type may already exist under " +
				"the same parent, or a rename or delete of it may still be running. Import an existing data object " +
				"rather than creating it again."
		}
	}

	return diag.NewErrorDiagnostic(summary, detail)
}
