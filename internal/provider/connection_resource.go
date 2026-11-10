package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
	_ resource.Resource                   = (*connectionResource)(nil)
	_ resource.ResourceWithConfigure      = (*connectionResource)(nil)
	_ resource.ResourceWithImportState    = (*connectionResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*connectionResource)(nil)
	_ resource.ResourceWithValidateConfig = (*connectionResource)(nil)
)

// How long the provider waits for Immuta to finish registering, updating or
// deleting a connection. Variables so that tests can shorten them.
var (
	connectionPollInterval  = 5 * time.Second
	connectionSettleTimeout = 10 * time.Minute
	connectionDeleteTimeout = 15 * time.Minute
)

// teardownWarningLimit caps the teardown instructions shown in a warning.
const teardownWarningLimit = 4096

// NewConnectionResource returns the immuta_connection resource.
func NewConnectionResource() resource.Resource {
	return &connectionResource{}
}

type connectionResource struct {
	client *immutaapi.Client
}

// connectionResourceModel is a connection registered with the connections
// API. The configuration and the credential are kept as the YAML or JSON
// documents the user wrote.
type connectionResourceModel struct {
	ID            types.String      `tfsdk:"id"`
	ConnectionKey types.String      `tfsdk:"connection_key"`
	Technology    types.String      `tfsdk:"technology"`
	Configuration yamlDocumentValue `tfsdk:"configuration"`
	Credential    yamlDocumentValue `tfsdk:"credential"`
	State         types.String      `tfsdk:"state"`
}

func (r *connectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connection"
}

func (r *connectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Registers an Immuta connection to a data platform with the connections API " +
			"(`/api/connections`).\n\n" +
			"**Destroying or replacing a connection deletes it and all of its data objects in Immuta.**",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The connection key.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"connection_key": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Unique key of the connection. It becomes the name of the top-level data " +
					"object in Immuta. Changing it replaces the connection.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"technology": schema.StringAttribute{
				Required: true,
				MarkdownDescription: fmt.Sprintf("Technology of the data platform: %s. Changing it replaces the connection.",
					codeList(connectionTechnologies)),
				Validators: []validator.String{
					stringvalidator.OneOf(connectionTechnologies...),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"configuration": schema.StringAttribute{
				CustomType: yamlDocumentType{},
				Required:   true,
				MarkdownDescription: "The rest of the registration payload, as a YAML or JSON document, usually read " +
					"with `file()`: `clientConfiguration` (without its `credential`), `dataObjectSync`, " +
					"`dataObjectDefaults`, `policyConfiguration`, `auditConfiguration` and `syncOnCreate`. " +
					"Formatting, comments and key order do not produce a diff.",
				PlanModifiers: []planmodifier.String{
					keepEquivalentYAMLDocument{},
				},
			},
			"credential": schema.StringAttribute{
				CustomType: yamlDocumentType{},
				Required:   true,
				Sensitive:  true,
				MarkdownDescription: "The `clientConfiguration.credential` of the payload, as a YAML or JSON " +
					"document, usually built with `jsonencode()` from variables. It needs an `authenticationType`. " +
					"Immuta never returns secrets, so changes made outside Terraform are not detected. Changing " +
					"it makes Immuta crawl the connection again.",
				PlanModifiers: []planmodifier.String{
					keepEquivalentYAMLDocument{},
				},
			},
			"state": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "State of the connection in Immuta, such as `ACTIVE` or `UNHEALTHY`, " +
					"read when the resource was last applied or refreshed.",
			},
		},
	}
}

func codeList(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = "`" + value + "`"
	}

	return strings.Join(quoted, ", ")
}

func (r *connectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

// ValidateConfig checks both documents at plan time, before any request.
func (r *connectionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var configuration, credential yamlDocumentValue

	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("configuration"), &configuration)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("credential"), &credential)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !configuration.IsNull() && !configuration.IsUnknown() {
		_, diags := readConnectionConfiguration(configuration)
		resp.Diagnostics.Append(diags...)
	}
	if !credential.IsNull() && !credential.IsUnknown() {
		_, diags := readConnectionCredential(credential)
		resp.Diagnostics.Append(diags...)
	}
}

// ModifyPlan replaces the connection when a setting the documented update
// cannot change is changed, and warns before any plan that deletes a
// connection, because that deletes all of its data objects too.
func (r *connectionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() {
		return
	}

	var state connectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key := state.ConnectionKey.ValueString()

	if req.Plan.Raw.IsNull() {
		resp.Diagnostics.AddWarning(
			fmt.Sprintf("Destroying the Immuta connection %s deletes all of its data objects", key),
			"Immuta deletes the connection together with every data object registered through it.",
		)

		return
	}

	var plan connectionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var reasons []string
	if !plan.ConnectionKey.IsUnknown() && plan.ConnectionKey.ValueString() != key {
		reasons = append(reasons, "connection_key")
	}
	if !plan.Technology.IsUnknown() && plan.Technology.ValueString() != state.Technology.ValueString() {
		reasons = append(reasons, "technology")
	}

	if !state.Configuration.IsNull() && !plan.Configuration.IsNull() && !plan.Configuration.IsUnknown() {
		previous, previousErr := parseYAMLMapping(state.Configuration.ValueString(), "configuration")
		next, nextErr := parseYAMLMapping(plan.Configuration.ValueString(), "configuration")
		if previousErr == nil && nextErr == nil {
			_, fixed := connectionChanges(plan.Technology.ValueString(), previous, next)
			if len(fixed) > 0 {
				resp.RequiresReplace = append(resp.RequiresReplace, path.Root("configuration"))
				reasons = append(reasons, fixed...)
			}
		}
	}

	if len(reasons) > 0 {
		resp.Diagnostics.AddWarning(
			fmt.Sprintf("Replacing the Immuta connection %s deletes all of its data objects", key),
			fmt.Sprintf("Immuta cannot change %s of an existing connection, so the connection is deleted, "+
				"together with every data object registered through it, and registered again.", strings.Join(reasons, ", ")),
		)
	}
}

func (r *connectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan connectionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	configuration, diags := readConnectionConfiguration(plan.Configuration)
	resp.Diagnostics.Append(diags...)
	credential, diags := readConnectionCredential(plan.Credential)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := plan.ConnectionKey.ValueString()
	body := connectionCreateBody(key, plan.Technology.ValueString(), configuration, credential)

	if _, err := r.client.CreateConnection(ctx, body); err != nil {
		resp.Diagnostics.Append(connectionErrorDiagnostic(fmt.Sprintf("Unable to register the Immuta connection %s", key), err))

		return
	}

	tflog.Debug(ctx, "Registered an Immuta connection", map[string]any{"connection_key": key})

	plan.ID = types.StringValue(key)
	resp.Diagnostics.Append(r.settle(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *connectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state connectionResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := state.ID.ValueString()

	connection, err := r.client.GetConnection(ctx, key)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			tflog.Debug(ctx, "Immuta connection no longer exists, removing it from state", map[string]any{"connection_key": key})
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.Append(connectionErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta connection %s", key), err))

		return
	}

	state.ConnectionKey = types.StringValue(connection.ConnectionKey)
	if connection.Technology != "" {
		state.Technology = types.StringValue(connection.Technology)
	}
	state.State = types.StringValue(connection.State)

	remote := remoteConnectionConfiguration(connection.Raw)

	var next map[string]any
	if state.Configuration.IsNull() {
		// Just imported: start from everything Immuta returns. Only the
		// settings in the configuration are compared from then on.
		next = remote
	} else if configured, err := parseYAMLMapping(state.Configuration.ValueString(), "configuration"); err == nil {
		if projected, _ := projectOntoRemote(configured, remote).(map[string]any); !equalJSON(projected, configured) {
			next = projected
		}
	}

	if next != nil {
		encoded, err := encodeJSONDocument(next)
		if err != nil {
			resp.Diagnostics.AddError("Unable to record the Immuta connection configuration", err.Error())

			return
		}
		state.Configuration = yamlDocumentValue{StringValue: types.StringValue(encoded)}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *connectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state connectionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	next, diags := readConnectionConfiguration(plan.Configuration)
	resp.Diagnostics.Append(diags...)
	credential, diags := readConnectionCredential(plan.Credential)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	previous := map[string]any{}
	if !state.Configuration.IsNull() {
		if parsed, err := parseYAMLMapping(state.Configuration.ValueString(), "configuration"); err == nil {
			previous = parsed
		}
	}

	patch, fixed := connectionChanges(plan.Technology.ValueString(), previous, next)
	if len(fixed) > 0 {
		// ModifyPlan replaces the connection instead; reaching this is a bug.
		resp.Diagnostics.AddError("Unable to update the Immuta connection",
			fmt.Sprintf("%s cannot be changed on an existing connection.", strings.Join(fixed, ", ")))

		return
	}

	// The credential is only sent when it changes, because Immuta crawls the
	// connection again whenever it receives one.
	if credentialChanged(state.Credential, plan.Credential) {
		client, _ := patch["clientConfiguration"].(map[string]any)
		if client == nil {
			client = map[string]any{}
		}
		client["credential"] = credential
		patch["clientConfiguration"] = client
	}

	key := state.ID.ValueString()

	if len(patch) > 0 {
		if _, err := r.client.UpdateConnection(ctx, key, patch); err != nil {
			resp.Diagnostics.Append(connectionErrorDiagnostic(fmt.Sprintf("Unable to update the Immuta connection %s", key), err))

			return
		}
	}

	plan.ID = state.ID
	resp.Diagnostics.Append(r.settle(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *connectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state connectionResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := state.ID.ValueString()

	deletion, err := r.client.DeleteConnection(ctx, key)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			return
		}

		resp.Diagnostics.Append(connectionErrorDiagnostic(fmt.Sprintf("Unable to delete the Immuta connection %s", key), err))

		return
	}

	if teardown := strings.TrimSpace(string(deletion.Teardown)); teardown != "" && teardown != "null" {
		if len(teardown) > teardownWarningLimit {
			teardown = teardown[:teardownWarningLimit] + "…"
		}
		resp.Diagnostics.AddWarning(
			fmt.Sprintf("Immuta returned teardown instructions for the connection %s", key),
			"Run them in the data platform to remove what Immuta created there:\n\n"+teardown,
		)
	}

	// Deletion is asynchronous. Waiting for it means a replacement can
	// register the same connection key again straight away.
	deadline := time.Now().Add(connectionDeleteTimeout)
	for {
		connection, err := r.client.GetConnection(ctx, key)
		if immutaapi.IsNotFound(err) {
			return
		}
		if err != nil {
			resp.Diagnostics.Append(connectionErrorDiagnostic(fmt.Sprintf("Unable to confirm the Immuta connection %s was deleted", key), err))

			return
		}
		if time.Now().After(deadline) {
			resp.Diagnostics.AddError(
				fmt.Sprintf("The Immuta connection %s is still being deleted", key),
				fmt.Sprintf("After %s the connection is still %s. Check it in Immuta, then refresh.", connectionDeleteTimeout, connection.State),
			)

			return
		}
		if err := sleepContext(ctx, connectionPollInterval); err != nil {
			resp.Diagnostics.AddError("Stopped waiting for the Immuta connection to be deleted", err.Error())

			return
		}
	}
}

// ImportState imports a connection by its key. Secrets cannot be read back,
// so the first apply after an import sends the configured credential.
func (r *connectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// settle waits until Immuta has finished registering or updating the
// connection, records its state, and warns when it is unhealthy or still
// busy. Neither is an error: the connection exists and can be fixed in place.
func (r *connectionResource) settle(ctx context.Context, model *connectionResourceModel) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	key := model.ID.ValueString()
	deadline := time.Now().Add(connectionSettleTimeout)

	for {
		connection, err := r.client.GetConnection(ctx, key)
		if err != nil {
			diagnostics.Append(connectionErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta connection %s", key), err))
			model.State = types.StringNull()

			return diagnostics
		}

		model.State = types.StringValue(connection.State)

		busy := connection.State == immutaapi.ConnectionStateActivating || connection.State == immutaapi.ConnectionStateUpdating
		switch {
		case connection.State == immutaapi.ConnectionStateUnhealthy:
			diagnostics.AddWarning(
				fmt.Sprintf("The Immuta connection %s is unhealthy", key),
				"Immuta saved the connection but reports it as UNHEALTHY. Check its health in Immuta; a wrong "+
					"credential or an unreachable host are common causes. Fix the configuration and apply again.",
			)

			return diagnostics
		case !busy:
			return diagnostics
		case time.Now().After(deadline):
			diagnostics.AddWarning(
				fmt.Sprintf("The Immuta connection %s is still %s", key, connection.State),
				fmt.Sprintf("Immuta had not finished after %s. Refresh later to see its state.", connectionSettleTimeout),
			)

			return diagnostics
		}

		if err := sleepContext(ctx, connectionPollInterval); err != nil {
			diagnostics.AddWarning("Stopped waiting for the Immuta connection", err.Error())

			return diagnostics
		}
	}
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// credentialChanged reports whether the credential differs from the one in
// state. A credential missing from state, after an import, has changed.
func credentialChanged(state, plan yamlDocumentValue) bool {
	if state.IsNull() {
		return true
	}

	equal, _ := state.StringSemanticEquals(context.Background(), plan)

	return !equal
}

func readConnectionConfiguration(value yamlDocumentValue) (map[string]any, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	document, err := parseYAMLMapping(value.ValueString(), "connection configuration")
	if err == nil {
		var warnings []string
		warnings, err = checkConnectionConfiguration(document)
		for _, warning := range warnings {
			diagnostics.AddAttributeWarning(path.Root("configuration"), "Undocumented connection key", warning)
		}
	}
	if err != nil {
		diagnostics.AddAttributeError(path.Root("configuration"), "Invalid connection configuration", err.Error())

		return nil, diagnostics
	}

	return document, diagnostics
}

// readConnectionCredential parses the credential. Its errors never include
// the document, which holds secrets.
func readConnectionCredential(value yamlDocumentValue) (map[string]any, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	document, err := parseYAMLMapping(value.ValueString(), "credential")
	if err != nil {
		diagnostics.AddAttributeError(path.Root("credential"), "Invalid connection credential",
			"The credential must be a single YAML or JSON mapping, such as the result of jsonencode().")

		return nil, diagnostics
	}
	if err := checkConnectionCredential(document); err != nil {
		diagnostics.AddAttributeError(path.Root("credential"), "Invalid connection credential", err.Error())

		return nil, diagnostics
	}

	return document, diagnostics
}

// connectionErrorDiagnostic turns an error from the connections API into an
// actionable diagnostic. API errors never contain credentials or full
// response payloads.
func connectionErrorDiagnostic(summary string, err error) diag.Diagnostic {
	detail := err.Error()

	var apiErr *immutaapi.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			detail += "\n\nCheck that the configured API key is valid and that the Immuta user holds the " +
				"APPLICATION_ADMIN permission needed to manage connections."
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			detail += "\n\nImmuta rejected the connection. Check the configuration and the credential against " +
				"the Immuta connections API reference; Immuta's reason is shown above."
		case http.StatusConflict:
			detail += "\n\nImmuta reported a conflict. A connection with this key may already exist, or another " +
				"operation on it may still be running. Import an existing connection rather than registering it again."
		}
	}

	return diag.NewErrorDiagnostic(summary, detail)
}
