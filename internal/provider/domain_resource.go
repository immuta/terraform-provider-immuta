package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
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
	_ resource.Resource                = (*domainResource)(nil)
	_ resource.ResourceWithConfigure   = (*domainResource)(nil)
	_ resource.ResourceWithImportState = (*domainResource)(nil)
)

// NewDomainResource returns the immuta_domain resource.
func NewDomainResource() resource.Resource {
	return &domainResource{}
}

type domainResource struct {
	client *immutaapi.Client
}

// domainResourceModel is the Terraform representation of a domain. It only
// holds attributes that are documented in the Immuta Domains API.
type domainResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Type           types.String `tfsdk:"type"`
	AssignmentType types.String `tfsdk:"assignment_type"`
	Autoscanning   types.Bool   `tfsdk:"autoscanning"`
	CreatedBy      types.Int64  `tfsdk:"created_by"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
	ForceDestroy   types.Bool   `tfsdk:"force_destroy"`
}

func (r *domainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (r *domainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an Immuta domain with the documented `/domain` API.\n\n" +
			"Creating a domain does not register data sources, enable Policy Sync, or author subscription or data policies. " +
			"Data source assignments, domain permissions and domain settings are managed separately.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Unique identifier of the domain, assigned by Immuta.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the domain. Changing the name updates the existing domain in place.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the domain that is displayed to users.",
			},
			"type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(immutaapi.DomainTypeDomain),
				MarkdownDescription: fmt.Sprintf("Type of the domain. The only documented value is `%s`, which is also the default. Changing this value replaces the domain.", immutaapi.DomainTypeDomain),
				Validators: []validator.String{
					stringvalidator.OneOf(immutaapi.DomainTypeDomain),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"assignment_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "How data sources are assigned to the domain, `manual` or `dynamic`. Managed through the documented assignment endpoint rather than this resource.",
			},
			"autoscanning": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Whether identification autoscanning is enabled for the domain, set with the documented " +
					"`PUT /domain/{domainId}/settings`. When omitted, the provider leaves the setting as it is and " +
					"reports Immuta's value; removing it from the configuration does not change the setting.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"created_by": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Profile ID of the user that created the domain.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timestamp of when the domain was created.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timestamp of when the domain was last updated.",
			},
			// No default, so that state written before the attribute existed,
			// and imported state, hold null and plan no change. Null means false.
			"force_destroy": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "When `true`, destroying the domain first removes every data source from it, with " +
					"`DELETE /collection/{domainId}/datasources/{dataSourceId}`, then deletes the domain. The data sources " +
					"themselves are not deleted. Global policies, identifiers and data products are not removed. Defaults " +
					"to `false`, in which case Immuta refuses to delete a domain that still has data sources. It must be " +
					"applied before the destroy that relies on it. Not sent to Immuta, and not set on import.",
			},
		},
	}
}

func (r *domainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*immutaapi.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *immutaapi.Client, got %T. This is a bug in the provider.", req.ProviderData),
		)

		return
	}

	r.client = client
}

func (r *domainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createRequest := immutaapi.CreateDomainRequest{
		Name: plan.Name.ValueString(),
		Type: plan.Type.ValueString(),
	}
	if !plan.Description.IsNull() {
		description := plan.Description.ValueString()
		createRequest.Description = &description
	}

	created, err := r.client.CreateDomain(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Unable to create the Immuta domain", err))

		return
	}

	tflog.Debug(ctx, "Created an Immuta domain", map[string]any{"id": created.ID})

	// The domain exists from here on. If applying the settings fails, it is
	// still saved to state, so that Terraform marks it tainted rather than
	// losing track of it.
	var settingsErr error
	if autoscanning, ok := knownBool(plan.Autoscanning); ok {
		settingsErr = r.client.SetDomainAutoscanning(ctx, created.ID, autoscanning)
	}

	// Refresh after create so that state matches what the API stored.
	domain, err := r.client.GetDomain(ctx, created.ID)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(
			fmt.Sprintf("Unable to read the Immuta domain %s back after creating it", created.ID),
			err,
		))
		if settingsErr == nil {
			return
		}
		// Save what is known, so that the created domain is tracked.
		domain = created
	}

	plan.applyDomain(domain)

	if settingsErr != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(
			fmt.Sprintf("Created the Immuta domain %s, but unable to set its autoscanning setting", created.ID),
			settingsErr,
		))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *domainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain, err := r.client.GetDomain(ctx, state.ID.ValueString())
	if err != nil {
		if immutaapi.IsNotFound(err) {
			tflog.Debug(ctx, "Immuta domain no longer exists, removing it from state", map[string]any{
				"id": state.ID.ValueString(),
			})
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.Append(apiErrorDiagnostic(
			fmt.Sprintf("Unable to read the Immuta domain %s", state.ID.ValueString()),
			err,
		))

		return
	}

	state.applyDomain(domain)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *domainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state domainResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domainID := state.ID.ValueString()

	// Only send attributes the documented PUT endpoint can change, and only
	// when they actually changed.
	var updateRequest immutaapi.UpdateDomainRequest
	if !plan.Name.Equal(state.Name) {
		name := plan.Name.ValueString()
		updateRequest.Name = &name
	}
	if !plan.Description.Equal(state.Description) {
		description := plan.Description.ValueString()
		updateRequest.Description = &description
	}

	if !updateRequest.IsEmpty() {
		if _, err := r.client.UpdateDomain(ctx, domainID, updateRequest); err != nil {
			resp.Diagnostics.Append(apiErrorDiagnostic(
				fmt.Sprintf("Unable to update the Immuta domain %s", domainID),
				err,
			))

			return
		}
	}

	// Settings have their own documented endpoint. Only a configured value
	// that differs from state is sent.
	if autoscanning, ok := knownBool(plan.Autoscanning); ok && !plan.Autoscanning.Equal(state.Autoscanning) {
		if err := r.client.SetDomainAutoscanning(ctx, domainID, autoscanning); err != nil {
			resp.Diagnostics.Append(apiErrorDiagnostic(
				fmt.Sprintf("Unable to update the autoscanning setting of the Immuta domain %s", domainID),
				err,
			))

			return
		}
	}

	// Refresh after update so that state matches what the API stored.
	domain, err := r.client.GetDomain(ctx, domainID)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(
			fmt.Sprintf("Unable to read the Immuta domain %s back after updating it", domainID),
			err,
		))

		return
	}

	plan.ID = state.ID
	plan.applyDomain(domain)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *domainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state domainResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domainID := state.ID.ValueString()
	// Null, in state written before force_destroy existed, means false.
	forceDestroy := state.ForceDestroy.ValueBool()

	if forceDestroy {
		exists, diags := r.removeDataSources(ctx, domainID)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() || !exists {
			return
		}
	}

	if err := r.client.DeleteDomain(ctx, domainID); err != nil {
		if immutaapi.IsNotFound(err) {
			// Already gone remotely, nothing left to do.
			return
		}

		resp.Diagnostics.Append(r.deleteErrorDiagnostic(ctx, domainID, forceDestroy, err))
	}
}

// removeDataSources empties the domain of data sources for force_destroy. It
// stops at the first failure, so that the domain is not deleted while data
// sources are still in it. It reports false when the domain no longer exists.
func (r *domainResource) removeDataSources(ctx context.Context, domainID string) (bool, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	dataSources, err := r.client.ListDomainDataSources(ctx, domainID, "")
	if err != nil {
		if immutaapi.IsNotFound(err) {
			return false, diagnostics
		}
		diagnostics.Append(apiErrorDiagnostic(
			fmt.Sprintf("Unable to list the data sources of the Immuta domain %s to remove them before deleting it", domainID),
			err,
		))

		return true, diagnostics
	}

	removed := make([]string, 0, len(dataSources))
	for _, dataSource := range dataSources {
		err := r.client.RemoveCollectionDataSource(ctx, domainID, dataSource.ID)
		if err != nil && !immutaapi.IsNotFound(err) {
			diagnostic := apiErrorDiagnostic(
				fmt.Sprintf("Unable to remove the data source %s from the Immuta domain %s before deleting it", dataSource.ID, domainID),
				err,
			)
			diagnostics.AddError(diagnostic.Summary(), diagnostic.Detail()+fmt.Sprintf("\n\nforce_destroy is set, so "+
				"the provider removes every data source from the domain before deleting it. It removed %d of the %d "+
				"data sources %q, then stopped; the domain was not deleted. Apply again to remove the rest.",
				len(removed), len(dataSources), removed))

			return true, diagnostics
		}

		removed = append(removed, dataSource.ID)
		tflog.Info(ctx, "Removed a data source from the Immuta domain before deleting it", map[string]any{
			"domain_id":      domainID,
			"data_source_id": dataSource.ID,
		})
	}

	return true, diagnostics
}

// deleteErrorDiagnostic explains a refused delete. Immuta refuses, with a 409,
// to delete a domain that still holds data sources, global policies,
// identifiers or data products; the dry run of the delete counts them.
func (r *domainResource) deleteErrorDiagnostic(ctx context.Context, domainID string, forceDestroy bool, err error) diag.Diagnostic {
	summary := fmt.Sprintf("Unable to delete the Immuta domain %s", domainID)

	var apiErr *immutaapi.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict {
		return apiErrorDiagnostic(summary, err)
	}

	detail := err.Error() + "\n\nImmuta does not delete a domain while anything is left in it."

	impact, previewErr := r.client.PreviewDomainDelete(ctx, domainID)
	if previewErr != nil {
		return diag.NewErrorDiagnostic(summary, detail+" Counting what is left with "+
			"DELETE /domain/"+domainID+"?dryRun=true failed too: "+previewErr.Error())
	}

	detail += fmt.Sprintf(" DELETE /domain/%s?dryRun=true reports: %d data sources (dataSourceCount), %d global "+
		"policies (globalPolicyCount), %d identifiers (identifierCount), %d data products (dataProductCount).",
		domainID, impact.DataSourceCount, impact.GlobalPolicyCount, impact.IdentifierCount, impact.DataProductCount)
	switch {
	case impact.DataSourceCount > 0 && forceDestroy:
		detail += "\n\nforce_destroy removed every data source the domain listed, but Immuta still counts some. " +
			"Immuta may still be removing them; apply again."
	case impact.DataSourceCount > 0:
		detail += "\n\nData sources added by a dynamic assignment (immuta_domain_dynamic_assignment) stay in the " +
			"domain when the assignment is destroyed. Remove them from the domain, or delete them, then apply again. " +
			"Alternatively, set force_destroy = true on the immuta_domain and apply that first: destroying the domain " +
			"then removes its data sources from it before deleting it."
	}
	if impact.GlobalPolicyCount+impact.IdentifierCount+impact.DataProductCount > 0 {
		detail += "\n\nDelete the global policies, identifiers and data products of the domain, or move them to " +
			"another domain, then apply again."
	}

	return diag.NewErrorDiagnostic(summary, detail)
}

func (r *domainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// applyDomain copies an API domain onto the Terraform model, normalizing
// optional values against whatever the model already holds.
func (m *domainResourceModel) applyDomain(domain *immutaapi.Domain) {
	m.ID = types.StringValue(domain.ID)
	m.Name = types.StringValue(domain.Name)
	m.Description = normalizeOptionalString(domain.Description, m.Description)
	m.Type = knownStringOr(domain.Type, m.Type)
	m.AssignmentType = optionalComputedString(domain.AssignmentType)
	m.UpdatedAt = optionalComputedString(domain.UpdatedAt)
	m.CreatedAt = optionalComputedString(domain.CreatedAt)

	// When the API omits the setting, a known value already on the model (a
	// configured or previously read one) is kept rather than nulled, so that
	// a response without settings never contradicts the configuration.
	if autoscanning, ok := domain.Autoscanning(); ok {
		m.Autoscanning = types.BoolValue(autoscanning)
	} else if _, known := knownBool(m.Autoscanning); !known {
		m.Autoscanning = types.BoolNull()
	}

	if domain.CreatedBy != nil {
		m.CreatedBy = types.Int64Value(*domain.CreatedBy)
	} else {
		m.CreatedBy = types.Int64Null()
	}
}

// normalizeOptionalString keeps omitted, null and empty API values consistent
// with what the practitioner configured, so that an unset optional attribute
// does not produce a perpetual diff.
func normalizeOptionalString(apiValue string, prior types.String) types.String {
	if apiValue != "" {
		return types.StringValue(apiValue)
	}

	if prior.IsNull() || prior.IsUnknown() {
		return types.StringNull()
	}

	// The practitioner configured an empty string, and the API agrees.
	return types.StringValue("")
}

// knownStringOr keeps a known configured value when the API omits it.
func knownStringOr(apiValue string, prior types.String) types.String {
	if apiValue != "" {
		return types.StringValue(apiValue)
	}

	if prior.IsUnknown() {
		return types.StringNull()
	}

	return prior
}

// optionalComputedString maps an omitted computed value to null rather than
// leaving it unknown.
func optionalComputedString(apiValue string) types.String {
	if apiValue == "" {
		return types.StringNull()
	}

	return types.StringValue(apiValue)
}

// apiErrorDiagnostic turns an API error into an actionable diagnostic. API
// errors never contain credentials or full response payloads.
func apiErrorDiagnostic(summary string, err error) diag.Diagnostic {
	detail := err.Error()

	var apiErr *immutaapi.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			detail += "\n\nCheck that the configured API key is valid and that the Immuta user holds the GOVERNANCE permission required by the domain endpoints."
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			detail += "\n\nImmuta rejected the request as invalid. Check the configured attribute values."
		case http.StatusConflict:
			detail += "\n\nImmuta reported a conflict. A domain with the same name may already exist, or the domain may still contain data sources, policies, data products or identifiers."
		}
	}

	return diag.NewErrorDiagnostic(summary, detail)
}

// knownBool returns a bool attribute's value when it is known and not null.
func knownBool(value types.Bool) (bool, bool) {
	if value.IsNull() || value.IsUnknown() {
		return false, false
	}

	return value.ValueBool(), true
}
