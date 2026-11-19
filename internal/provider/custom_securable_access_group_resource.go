package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                = (*customSecurableAccessGroupResource)(nil)
	_ resource.ResourceWithConfigure   = (*customSecurableAccessGroupResource)(nil)
	_ resource.ResourceWithImportState = (*customSecurableAccessGroupResource)(nil)
)

// NewCustomSecurableAccessGroupResource returns the
// immuta_custom_securable_access_group resource.
func NewCustomSecurableAccessGroupResource() resource.Resource {
	return &customSecurableAccessGroupResource{}
}

type customSecurableAccessGroupResource struct {
	client *immutaapi.Client
}

type customSecurableAccessGroupResourceModel struct {
	ID              types.String `tfsdk:"id"`
	ConnectionKey   types.String `tfsdk:"connection_key"`
	SecurableID     types.String `tfsdk:"securable_id"`
	IdentityGroupID types.String `tfsdk:"identity_group_id"`
}

func (r *customSecurableAccessGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_securable_access_group"
}

func (r *customSecurableAccessGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Binds an identity group, crawled by an Entra or Okta connection, to a Custom securable. When " +
			"a subscription policy or an approved access request grants a user the securable, Immuta adds the user to " +
			"the group through the identity connection, and removes them when access is revoked. The binding itself " +
			"grants nobody access.\n\n" +
			"A securable has at most one bound group, and a group can be bound to only one securable.\n\n" +
			"Destroying this resource unbinds the group. Immuta does not delete a securable while a group is bound, " +
			"so Terraform destroys the binding before the securable it references.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`<connection_key>/<securable_id>`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"connection_key": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Key of the Custom connection that holds the securable, usually " +
					"`immuta_custom_data_object.<name>.connection_key`. Changing it replaces the binding.",
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: replace,
			},
			"securable_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "ID of the Custom securable, usually `immuta_custom_data_object.<name>.id`. Changing " +
					"it replaces the binding.",
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: replace,
			},
			"identity_group_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Immuta data object ID of the identity group, usually " +
					"`data.immuta_data_object.<name>.id` looked up in the Entra or Okta connection. It is not the group's " +
					"ID in the identity provider. Changing it replaces the binding.",
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: replace,
			},
		},
	}
}

func (r *customSecurableAccessGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *customSecurableAccessGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan customSecurableAccessGroupResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, securableID, groupID := plan.ConnectionKey.ValueString(), plan.SecurableID.ValueString(), plan.IdentityGroupID.ValueString()
	id := customSecurableAccessGroupID(key, securableID)

	// A binding to the same group is taken over, for example after the
	// resource was removed from the state. A binding to another group is not
	// replaced silently: it may have been made outside Terraform.
	bound, err := r.client.GetAccessGroup(ctx, key, securableID)
	if err != nil {
		resp.Diagnostics.Append(customDataObjectErrorDiagnostic(
			fmt.Sprintf("Unable to read the access group of the Immuta securable %s", securableID), err))

		return
	}
	switch bound.IdentityGroupID {
	case "":
	case groupID:
		tflog.Debug(ctx, "The identity group is already bound to the Immuta securable", map[string]any{"id": id})
		plan.ID = types.StringValue(id)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

		return
	default:
		resp.Diagnostics.AddError("The Immuta securable already has another access group",
			fmt.Sprintf("Immuta reports %s bound to the securable %s, so Terraform does not bind the identity group "+
				"%s instead. Import the binding with terraform import using the ID %s and change identity_group_id, "+
				"or remove the binding in the Immuta UI, then apply again.", bound, securableID, groupID, id))

		return
	}

	if err := r.client.BindAccessGroup(ctx, key, securableID, groupID); err != nil {
		resp.Diagnostics.Append(accessGroupErrorDiagnostic(
			fmt.Sprintf("Unable to bind the identity group %s to the Immuta securable %s", groupID, securableID), err))

		return
	}

	tflog.Debug(ctx, "Bound an identity group to an Immuta Custom securable",
		map[string]any{"connection_key": key, "securable_id": securableID, "identity_group_id": groupID})

	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *customSecurableAccessGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state customSecurableAccessGroupResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, securableID := state.ConnectionKey.ValueString(), state.SecurableID.ValueString()

	bound, err := r.client.GetAccessGroup(ctx, key, securableID)
	if err != nil && !immutaapi.IsNotFound(err) {
		resp.Diagnostics.Append(customDataObjectErrorDiagnostic(
			fmt.Sprintf("Unable to read the access group of the Immuta securable %s", securableID), err))

		return
	}
	if !bound.Bound() {
		tflog.Debug(ctx, "No identity group bound to the Immuta securable any more, removing it from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)

		return
	}

	state.ID = types.StringValue(customSecurableAccessGroupID(key, securableID))
	state.IdentityGroupID = types.StringValue(bound.IdentityGroupID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called with a change: every configurable attribute
// requires replacement.
func (r *customSecurableAccessGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan customSecurableAccessGroupResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete unbinds the group. A binding, or securable, that Immuta no longer
// finds is already gone.
func (r *customSecurableAccessGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state customSecurableAccessGroupResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, securableID := state.ConnectionKey.ValueString(), state.SecurableID.ValueString()

	err := r.client.UnbindAccessGroup(ctx, key, securableID)
	if immutaapi.IsNotFound(err) {
		tflog.Debug(ctx, "The Immuta access group binding is already gone", map[string]any{"id": state.ID.ValueString()})

		return
	}
	if err != nil {
		resp.Diagnostics.Append(customDataObjectErrorDiagnostic(
			fmt.Sprintf("Unable to unbind the identity group %s from the Immuta securable %s",
				state.IdentityGroupID.ValueString(), securableID), err))

		return
	}

	tflog.Debug(ctx, "Unbound an identity group from an Immuta Custom securable",
		map[string]any{"connection_key": key, "securable_id": securableID, "identity_group_id": state.IdentityGroupID.ValueString()})
}

// ImportState imports a binding by <connection_key>/<securable_id>; the
// bound group is read from Immuta.
func (r *customSecurableAccessGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	key, securableID, ok := strings.Cut(req.ID, "/")
	if !ok || key == "" || securableID == "" || strings.Contains(securableID, "/") {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Expected <connection_key>/<securable_id>, got %q.", req.ID))

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("connection_key"), key)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("securable_id"), securableID)...)
}

func customSecurableAccessGroupID(connectionKey, securableID string) string {
	return connectionKey + "/" + securableID
}

// accessGroupErrorDiagnostic explains the documented rules of a binding.
func accessGroupErrorDiagnostic(summary string, err error) diag.Diagnostic {
	var apiErr *immutaapi.APIError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusBadRequest || apiErr.StatusCode == http.StatusConflict) {
		return diag.NewErrorDiagnostic(summary, err.Error()+"\n\nThe securable must be a SECURABLE in a Custom "+
			"connection, and identity_group_id the Immuta data object ID of a group crawled by an Entra or Okta "+
			"connection that is not bound to another securable.")
	}

	return customDataObjectErrorDiagnostic(summary, err)
}
