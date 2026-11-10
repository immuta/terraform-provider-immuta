package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ resource.Resource                = (*dataObjectSettingsResource)(nil)
	_ resource.ResourceWithConfigure   = (*dataObjectSettingsResource)(nil)
	_ resource.ResourceWithImportState = (*dataObjectSettingsResource)(nil)
)

// How long the provider waits for a data object to appear, after the
// connection that holds it is registered or synchronised. Variables so that
// tests can shorten them.
var (
	dataObjectPollInterval  = 5 * time.Second
	dataObjectLookupTimeout = 10 * time.Minute
)

// NewDataObjectSettingsResource returns the immuta_data_object_settings
// resource.
func NewDataObjectSettingsResource() resource.Resource {
	return &dataObjectSettingsResource{}
}

type dataObjectSettingsResource struct {
	client *immutaapi.Client
}

type dataObjectSettingsResourceModel struct {
	ID                  types.String `tfsdk:"id"`
	ConnectionKey       types.String `tfsdk:"connection_key"`
	Path                types.List   `tfsdk:"path"`
	Type                types.String `tfsdk:"type"`
	SyncPolicy          types.Bool   `tfsdk:"sync_policy"`
	SyncPolicyInherited types.Bool   `tfsdk:"sync_policy_inherited"`
}

func (r *dataObjectSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_object_settings"
}

func (r *dataObjectSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the settings of a data object (a catalog, database, schema or table) that a connection " +
			"registered, with the documented connections API. It does not create or delete the data object.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ID of the data object.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"connection_key": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Key of the connection that holds the data object, usually " +
					"`immuta_connection.<name>.connection_key`, so that the data object is looked up after the connection " +
					"is registered. Changing it manages another data object.",
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"path": schema.ListAttribute{
				ElementType: types.StringType,
				Required:    true,
				MarkdownDescription: "Names from the top of the connection down to the data object, for example " +
					"`[\"acme_dev\"]` for a catalog or `[\"acme_dev\", \"sales\", \"orders\"]` for a table. Changing it " +
					"manages another data object.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Type of the data object, for example `CATALOG`, `SCHEMA` or `TABLE`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"sync_policy": schema.BoolAttribute{
				Required: true,
				MarkdownDescription: "Whether Immuta applies data policies to the data object (the \"Data policy\" setting). " +
					"Set on a catalog, database or schema, it is inherited by the objects under it that do not set it.",
			},
			"sync_policy_inherited": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether Immuta reports `sync_policy` as inherited from a parent data object or the " +
					"connection rather than set on this data object.",
			},
		},
	}
}

func (r *dataObjectSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *dataObjectSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dataObjectSettingsResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var names []string
	resp.Diagnostics.Append(plan.Path.ElementsAs(ctx, &names, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := plan.ConnectionKey.ValueString()
	object, diags := findDataObject(ctx, r.client, key, names)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Found the Immuta data object", map[string]any{"connection_key": key, "id": object.ID})

	if err := r.client.SetDataObjectSyncPolicy(ctx, key, object.ID, plan.SyncPolicy.ValueBoolPointer()); err != nil {
		resp.Diagnostics.Append(connectionErrorDiagnostic(fmt.Sprintf("Unable to update the settings of the Immuta data object %s", formatObjectPath(names)), err))

		return
	}

	plan.ID = types.StringValue(object.ID)
	if found, diags := r.refresh(ctx, &plan); diags.HasError() || !found {
		resp.Diagnostics.Append(diags...)
		if !found {
			resp.Diagnostics.AddError("The Immuta data object disappeared",
				fmt.Sprintf("The data object %s was deleted while its settings were being updated.", formatObjectPath(names)))
		}

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dataObjectSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dataObjectSettingsResourceModel

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
		tflog.Debug(ctx, "Immuta data object no longer exists, removing it from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *dataObjectSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state dataObjectSettingsResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, id := state.ConnectionKey.ValueString(), state.ID.ValueString()
	if err := r.client.SetDataObjectSyncPolicy(ctx, key, id, plan.SyncPolicy.ValueBoolPointer()); err != nil {
		resp.Diagnostics.Append(connectionErrorDiagnostic(fmt.Sprintf("Unable to update the settings of the Immuta data object %s", id), err))

		return
	}

	plan.ID = state.ID
	if found, diags := r.refresh(ctx, &plan); diags.HasError() || !found {
		resp.Diagnostics.Append(diags...)
		if !found {
			resp.Diagnostics.AddError("The Immuta data object disappeared",
				fmt.Sprintf("The data object %s was deleted while its settings were being updated.", id))
		}

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete clears the settings this resource set, so that the data object
// inherits them again. The data object itself belongs to the connection.
func (r *dataObjectSettingsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dataObjectSettingsResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, id := state.ConnectionKey.ValueString(), state.ID.ValueString()
	if err := r.client.SetDataObjectSyncPolicy(ctx, key, id, nil); err != nil && !immutaapi.IsNotFound(err) {
		resp.Diagnostics.Append(connectionErrorDiagnostic(fmt.Sprintf("Unable to clear the settings of the Immuta data object %s", id), err))
	}
}

// ImportState imports the settings of a data object by
// <connection_key>/<data object id>.
func (r *dataObjectSettingsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	separator := strings.LastIndex(req.ID, "/")
	if separator <= 0 || separator == len(req.ID)-1 {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Expected <connection_key>/<data object id>, got %q.", req.ID))

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("connection_key"), req.ID[:separator])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID[separator+1:])...)
}

// findDataObject looks a data object up by path. A connection registers its
// data objects asynchronously, so a data object that is not there yet is
// waited for; several data objects with the path is an error.
func findDataObject(ctx context.Context, client *immutaapi.Client, key string, names []string) (*immutaapi.DataObject, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	deadline := time.Now().Add(dataObjectLookupTimeout)
	for {
		candidates, err := client.SearchDataObjects(ctx, key, names[len(names)-1])
		if err != nil {
			diagnostics.Append(connectionErrorDiagnostic(fmt.Sprintf("Unable to look up the Immuta data object %s", formatObjectPath(names)), err))

			return nil, diagnostics
		}

		var matches []immutaapi.DataObject
		for _, candidate := range candidates {
			if slices.Equal(candidate.PathNames(), names) {
				matches = append(matches, candidate)
			}
		}

		switch {
		case len(matches) == 1:
			return &matches[0], diagnostics
		case len(matches) > 1:
			diagnostics.AddError("Several Immuta data objects found",
				fmt.Sprintf("%d data objects of the connection %s have the path %s. Import the one to manage with "+
					"terraform import, using <connection_key>/<data object id>.", len(matches), key, formatObjectPath(names)))

			return nil, diagnostics
		case time.Now().After(deadline):
			detail := fmt.Sprintf("The connection %s has no data object with the path %s after %s. Check the path: it "+
				"holds the names from the top of the connection down to the data object, and is case-sensitive.",
				key, formatObjectPath(names), dataObjectLookupTimeout)
			if len(candidates) > 0 {
				var found []string
				for _, candidate := range candidates {
					found = append(found, formatObjectPath(candidate.PathNames()))
				}
				slices.Sort(found)
				detail += fmt.Sprintf(" Data objects with the same name: %s.", strings.Join(found, ", "))
			}
			diagnostics.AddError("Immuta data object not found", detail)

			return nil, diagnostics
		}

		tflog.Debug(ctx, "Waiting for the Immuta data object to be registered", map[string]any{"connection_key": key, "path": names})
		if err := sleepContext(ctx, dataObjectPollInterval); err != nil {
			diagnostics.AddError("Stopped waiting for the Immuta data object", err.Error())

			return nil, diagnostics
		}
	}
}

// refresh reads the data object into the model. It reports false when the
// data object no longer exists.
func (r *dataObjectSettingsResource) refresh(ctx context.Context, model *dataObjectSettingsResourceModel) (bool, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	key, id := model.ConnectionKey.ValueString(), model.ID.ValueString()
	object, err := r.client.GetDataObject(ctx, key, id)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			return false, diagnostics
		}
		diagnostics.Append(connectionErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta data object %s", id), err))

		return true, diagnostics
	}

	names, diags := types.ListValueFrom(ctx, types.StringType, object.PathNames())
	diagnostics.Append(diags...)

	model.Path = names
	model.Type = types.StringValue(object.Type)
	model.SyncPolicy = types.BoolPointerValue(object.SyncPolicy)
	model.SyncPolicyInherited = types.BoolValue(object.SyncPolicyInherited)

	return true, diagnostics
}

func formatObjectPath(names []string) string {
	return strings.Join(names, ".")
}
