package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ resource.Resource                = (*dataProductResource)(nil)
	_ resource.ResourceWithConfigure   = (*dataProductResource)(nil)
	_ resource.ResourceWithImportState = (*dataProductResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*dataProductResource)(nil)
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// NewDataProductResource returns the immuta_data_product resource.
func NewDataProductResource() resource.Resource {
	return &dataProductResource{}
}

type dataProductResource struct {
	client *immutaapi.Client
}

type dataProductResourceModel struct {
	ID                    types.String `tfsdk:"id"`
	Name                  types.String `tfsdk:"name"`
	Description           types.String `tfsdk:"description"`
	Domains               types.Set    `tfsdk:"domains"`
	SubjectMatterExpertID types.String `tfsdk:"subject_matter_expert_id"`
	DataSources           types.Set    `tfsdk:"data_sources"`
	Suspended             types.Bool   `tfsdk:"suspended"`

	DataAccessRequestFormID       types.String `tfsdk:"data_access_request_form_id"`
	MaskingExceptionRequestFormID types.String `tfsdk:"masking_exception_request_form_id"`
}

func (r *dataProductResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_product"
}

func (r *dataProductResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	uuid := stringvalidator.RegexMatches(uuidPattern, "must be a UUID, such as 4c8f6d82-e4c6-4478-92eb-d9342500f006")

	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an Immuta data product with the Request app (formerly Marketplace) API.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "ID of the data product, a UUID. Set it to keep the same ID in every environment; " +
					"otherwise Immuta assigns one. Changing it replaces the data product.",
				Validators: []validator.String{uuid},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the data product. Immuta requires it to be unique.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the data product.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"domains": schema.SetAttribute{
				ElementType: types.StringType,
				Required:    true,
				MarkdownDescription: "IDs of the domains the data product belongs to, usually `immuta_domain.<name>.id`. " +
					"At least one is required.",
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
			"subject_matter_expert_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Immuta global user ID (a UUID) of the data product's subject matter expert. " +
					"This is not the numeric profile ID that `immuta_user.id` holds.",
				Validators: []validator.String{uuid},
			},
			"data_sources": schema.SetAttribute{
				ElementType: types.StringType,
				Required:    true,
				MarkdownDescription: "IDs of the Immuta data sources in the data product, for example from " +
					"`data.immuta_domain_data_sources.<name>.ids`.",
				Validators: []validator.Set{
					setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
			"suspended": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Whether access to the data product's data sources is suspended. Defaults to `false`.",
			},
			"data_access_request_form_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "ID of the request form users fill in to request access to the data product, for example " +
					"`data.immuta_request_form.<name>.id`. Required, with `masking_exception_request_form_id`, to create a data " +
					"product. Immuta does not return it, so changes made outside Terraform are not detected.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.AlsoRequires(path.MatchRoot("masking_exception_request_form_id")),
				},
			},
			"masking_exception_request_form_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "ID of the request form users fill in to request a masking exception, for example " +
					"`data.immuta_request_form.<name>.id`; it can be the same form as `data_access_request_form_id`. Required, " +
					"with `data_access_request_form_id`, to create a data product. Immuta does not return it, so changes made " +
					"outside Terraform are not detected.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.AlsoRequires(path.MatchRoot("data_access_request_form_id")),
				},
			},
		},
	}
}

func (r *dataProductResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
	requireRequestApp(r.client, "immuta_data_product", &resp.Diagnostics)
}

// ModifyPlan stops a data product from being planned for creation without
// request forms, which Immuta requires, so that the error shows at plan time.
// Existing data products, for example imported ones, may leave them out.
func (r *dataProductResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if !req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var formID types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("data_access_request_form_id"), &formID)...)
	if resp.Diagnostics.HasError() || !formID.IsNull() {
		return
	}

	resp.Diagnostics.AddAttributeError(path.Root("data_access_request_form_id"), "Request forms required",
		"Immuta requires request forms to create a data product: set data_access_request_form_id and "+
			"masking_exception_request_form_id, for example from data.immuta_request_form. Both can be the same form.")
}

func (r *dataProductResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dataProductResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domains, sourceIDs, diags := plan.sets(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createRequest := immutaapi.CreateDataProductRequest{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
		Metadata:    dataProductMetadata(plan.SubjectMatterExpertID),
		Domains:     domains,
		DataSources: immutaapi.DataProductSources(sourceIDs),

		DataAccessRequestFormID:       plan.DataAccessRequestFormID.ValueString(),
		MaskingExceptionRequestFormID: plan.MaskingExceptionRequestFormID.ValueString(),
	}
	if isSet(plan.ID) {
		createRequest.ID = plan.ID.ValueString()
	}

	product, err := r.client.CreateDataProduct(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.Append(dataProductErrorDiagnostic(fmt.Sprintf("Unable to create the Immuta data product %q", createRequest.Name), err))

		return
	}

	tflog.Debug(ctx, "Created an Immuta data product", map[string]any{"id": product.ID})

	if createRequest.ID != "" && product.ID != createRequest.ID {
		// Save what exists, so that it is tracked and can be fixed.
		plan.ID = types.StringValue(product.ID)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		resp.Diagnostics.AddError("Immuta assigned another data product id",
			fmt.Sprintf("The configuration asks for the id %s, but Immuta created the data product as %s.", createRequest.ID, product.ID))

		return
	}
	plan.ID = types.StringValue(product.ID)

	// The create payload has no suspended, so a data product that starts
	// suspended is suspended straight after.
	if plan.Suspended.ValueBool() {
		if err := r.client.UpdateDataProduct(ctx, product.ID, plan.updateRequest(domains)); err != nil {
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			resp.Diagnostics.Append(dataProductErrorDiagnostic(fmt.Sprintf("Unable to suspend the Immuta data product %s", product.ID), err))

			return
		}
	}

	// Refresh after create so that state matches what the API stored.
	if _, diags := r.refresh(ctx, &plan); diags.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		resp.Diagnostics.Append(diags...)

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dataProductResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dataProductResourceModel

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
		tflog.Debug(ctx, "Immuta data product no longer exists, removing it from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *dataProductResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state dataProductResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domains, sourceIDs, diags := plan.sets(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	plan.ID = state.ID

	// Every managed field is sent, so that what Immuta stores matches the
	// configuration.
	if !plan.Name.Equal(state.Name) || !plan.Description.Equal(state.Description) || !plan.Domains.Equal(state.Domains) ||
		!plan.SubjectMatterExpertID.Equal(state.SubjectMatterExpertID) || !plan.Suspended.Equal(state.Suspended) ||
		!plan.DataAccessRequestFormID.Equal(state.DataAccessRequestFormID) ||
		!plan.MaskingExceptionRequestFormID.Equal(state.MaskingExceptionRequestFormID) {
		if err := r.client.UpdateDataProduct(ctx, id, plan.updateRequest(domains)); err != nil {
			resp.Diagnostics.Append(dataProductErrorDiagnostic(fmt.Sprintf("Unable to update the Immuta data product %s", id), err))

			return
		}
	}

	if !plan.DataSources.Equal(state.DataSources) {
		if err := r.client.SetDataProductDataSources(ctx, id, sourceIDs); err != nil {
			resp.Diagnostics.Append(dataProductErrorDiagnostic(fmt.Sprintf("Unable to update the data sources of the Immuta data product %s", id), err))

			return
		}
	}

	// Refresh after update so that state matches what the API stored.
	if _, diags := r.refresh(ctx, &plan); diags.HasError() {
		resp.Diagnostics.Append(diags...)

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dataProductResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dataProductResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	if err := r.client.DeleteDataProduct(ctx, id); err != nil && !immutaapi.IsNotFound(err) {
		resp.Diagnostics.Append(dataProductErrorDiagnostic(fmt.Sprintf("Unable to delete the Immuta data product %s", id), err))
	}
}

// ImportState imports a data product by its id.
func (r *dataProductResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// refresh reads the data product and its data sources into the model. It
// reports false when the data product no longer exists.
func (r *dataProductResource) refresh(ctx context.Context, model *dataProductResourceModel) (bool, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	id := model.ID.ValueString()

	product, err := r.client.GetDataProduct(ctx, id)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			return false, diagnostics
		}
		diagnostics.Append(dataProductErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta data product %s", id), err))

		return true, diagnostics
	}

	sourceIDs, err := r.client.GetDataProductDataSources(ctx, id)
	if err != nil {
		diagnostics.Append(dataProductErrorDiagnostic(fmt.Sprintf("Unable to read the data sources of the Immuta data product %s", id), err))

		return true, diagnostics
	}

	diagnostics.Append(model.apply(ctx, product, sourceIDs)...)

	return true, diagnostics
}

// apply copies what Immuta returned into the model. Empty descriptions are
// held as null, and domains and data sources as sets, so that neither the
// API's ordering nor an empty string produces a diff. Immuta does not return
// the request form ids, so they keep their configured values.
func (m *dataProductResourceModel) apply(ctx context.Context, product *immutaapi.DataProduct, sourceIDs []string) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	m.ID = types.StringValue(product.ID)
	m.Name = types.StringValue(product.Name)
	m.Description = types.StringNull()
	if product.Description != nil && *product.Description != "" {
		m.Description = types.StringValue(*product.Description)
	}
	m.SubjectMatterExpertID = types.StringValue(product.SubjectMatterExpertID())
	m.Suspended = types.BoolValue(product.Suspended)

	domains, diags := types.SetValueFrom(ctx, types.StringType, nonNil(product.Domains))
	diagnostics.Append(diags...)
	m.Domains = domains

	dataSources, diags := types.SetValueFrom(ctx, types.StringType, nonNil(sourceIDs))
	diagnostics.Append(diags...)
	m.DataSources = dataSources

	return diagnostics
}

// sets returns the configured domains and data source ids, sorted so that
// requests are deterministic.
func (m *dataProductResourceModel) sets(ctx context.Context) ([]string, []string, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	var domains, sourceIDs []string

	diagnostics.Append(m.Domains.ElementsAs(ctx, &domains, false)...)
	diagnostics.Append(m.DataSources.ElementsAs(ctx, &sourceIDs, false)...)
	slices.Sort(domains)
	slices.Sort(sourceIDs)

	return domains, sourceIDs, diagnostics
}

func (m *dataProductResourceModel) updateRequest(domains []string) immutaapi.UpdateDataProductRequest {
	return immutaapi.UpdateDataProductRequest{
		Name:        m.Name.ValueString(),
		Description: m.Description.ValueStringPointer(),
		Metadata:    dataProductMetadata(m.SubjectMatterExpertID),
		Domains:     domains,
		Suspended:   m.Suspended.ValueBool(),

		DataAccessRequestFormID:       m.DataAccessRequestFormID.ValueString(),
		MaskingExceptionRequestFormID: m.MaskingExceptionRequestFormID.ValueString(),
	}
}

func dataProductMetadata(subjectMatterExpertID types.String) immutaapi.DataProductMetadata {
	return immutaapi.DataProductMetadata{
		SubjectMatterExpert: immutaapi.DataProductSubjectMatterExpert{ID: subjectMatterExpertID.ValueString()},
	}
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}

	return values
}

// dataProductErrorDiagnostic turns an error from the Request app API into an
// actionable diagnostic. API errors never contain credentials or full
// response payloads.
func dataProductErrorDiagnostic(summary string, err error) diag.Diagnostic {
	detail := err.Error()

	var apiErr *immutaapi.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized:
			detail += "\n\n" + requestAppUnauthorizedHint
		case http.StatusForbidden, http.StatusNotFound:
			detail += "\n\nCheck that the Immuta user who owns the personal access token can manage data products " +
				"in the domains used, and that `request_app_url` is the regional URL of the tenant's region."
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			detail += "\n\nImmuta rejected the data product. Check that the domains, data sources, subject " +
				"matter expert and request forms exist; Immuta's reason is shown above."
		case http.StatusConflict:
			detail += "\n\nImmuta reported a conflict: the data product name is already used. Import the " +
				"existing data product, or choose another name."
		}
	}

	return diag.NewErrorDiagnostic(summary, detail)
}
