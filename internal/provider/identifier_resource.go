package provider

import (
	"context"
	"fmt"

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
	_ resource.Resource                   = (*identifierResource)(nil)
	_ resource.ResourceWithConfigure      = (*identifierResource)(nil)
	_ resource.ResourceWithImportState    = (*identifierResource)(nil)
	_ resource.ResourceWithValidateConfig = (*identifierResource)(nil)
)

// NewIdentifierResource returns the immuta_identifier resource.
func NewIdentifierResource() resource.Resource {
	return &identifierResource{}
}

type identifierResource struct {
	client *immutaapi.Client
}

// identifierResourceModel is the Terraform representation of an identifier.
type identifierResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Description   types.String `tfsdk:"description"`
	DomainID      types.String `tfsdk:"domain_id"`
	Type          types.String `tfsdk:"type"`
	Regex         types.String `tfsdk:"regex"`
	Values        types.Set    `tfsdk:"values"`
	CaseSensitive types.Bool   `tfsdk:"case_sensitive"`
	Tags          types.Set    `tfsdk:"tags"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

func (r *identifierResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_identifier"
}

func (r *identifierResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an Immuta identifier with the documented `/sdd/identifier` API.\n\n" +
			"An identifier tells sensitive data discovery what to look for and which tags to apply when it finds it. " +
			"Set `domain_id` to create the identifier inside a domain, or leave it unset to create a reference identifier " +
			"available across the tenant.\n\n" +
			"Every tag in `tags` must already exist. Reference `immuta_tag` rather than typing names so that Terraform " +
			"creates the tags first.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Unique ID of the identifier, assigned by Immuta.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the identifier.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"description": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Description of the identifier. Immuta requires one.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"domain_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "ID of the domain to create the identifier in, for example `immuta_domain.finance.id`. " +
					"Omit to create a reference identifier that is not tied to a domain. Changing it replaces the identifier.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Type of criteria the identifier uses. `regex` matches column values, " +
					"`columnNameRegex` matches column names, and `dictionary` matches column values against a list.",
				Validators: []validator.String{
					stringvalidator.OneOf(immutaapi.IdentifierTypes()...),
				},
			},
			"regex": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Case-insensitive regular expression to match. Required when `type` is `regex` or " +
					"`columnNameRegex`, and must not be set otherwise.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"values": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Values the dictionary matches against column values. Required when `type` is " +
					"`dictionary`, and must not be set otherwise.",
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
				},
			},
			"case_sensitive": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Whether dictionary values are matched case-sensitively. Only applies when `type` " +
					"is `dictionary`. Defaults to `false`.",
			},
			"tags": schema.SetAttribute{
				Required:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Fully qualified names of the tags to apply when the criteria matches. Each tag must " +
					"already exist in Immuta; use `immuta_tag` to create it.",
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
				},
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timestamp of when the identifier was created.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timestamp of when the identifier was last updated.",
			},
		},
	}
}

// ValidateConfig enforces the documented pairing of type and config, which
// the API only reports once the request has been sent.
func (r *identifierResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config identifierResourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.Type.IsUnknown() || config.Type.IsNull() {
		return
	}

	hasRegex := isSet(config.Regex)
	hasValues := !config.Values.IsNull() && !config.Values.IsUnknown()

	switch config.Type.ValueString() {
	case immutaapi.IdentifierTypeRegex, immutaapi.IdentifierTypeColumnNameRegex:
		if !hasRegex && !config.Regex.IsUnknown() {
			resp.Diagnostics.AddAttributeError(
				path.Root("regex"),
				"Missing regex",
				fmt.Sprintf("An identifier of type %q needs a regular expression in regex.", config.Type.ValueString()),
			)
		}
		if hasValues {
			resp.Diagnostics.AddAttributeError(
				path.Root("values"),
				"Unexpected values",
				fmt.Sprintf("values only applies to an identifier of type %q. Remove it or change type.", immutaapi.IdentifierTypeDictionary),
			)
		}
	case immutaapi.IdentifierTypeDictionary:
		if !hasValues {
			resp.Diagnostics.AddAttributeError(
				path.Root("values"),
				"Missing values",
				fmt.Sprintf("An identifier of type %q needs at least one entry in values.", immutaapi.IdentifierTypeDictionary),
			)
		}
		if hasRegex {
			resp.Diagnostics.AddAttributeError(
				path.Root("regex"),
				"Unexpected regex",
				fmt.Sprintf("regex does not apply to an identifier of type %q. Remove it or change type.", immutaapi.IdentifierTypeDictionary),
			)
		}
	}
}

func (r *identifierResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *identifierResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan identifierResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	saveRequest, diags := plan.saveRequest(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateIdentifier(ctx, saveRequest)
	if err != nil {
		resp.Diagnostics.Append(identifierErrorDiagnostic("Unable to create the Immuta identifier", err))

		return
	}

	tflog.Debug(ctx, "Created an Immuta identifier", map[string]any{"id": created.ID})

	resp.Diagnostics.Append(plan.applyIdentifier(ctx, created)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *identifierResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state identifierResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	identifier, err := r.client.GetIdentifier(ctx, id)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			tflog.Debug(ctx, "Immuta identifier no longer exists, removing it from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.Append(identifierErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta identifier %s", id), err))

		return
	}

	resp.Diagnostics.Append(state.applyIdentifier(ctx, identifier)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *identifierResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state identifierResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// Immuta does not support partial updates of an identifier, so the whole
	// thing is sent every time.
	saveRequest, diags := plan.saveRequest(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updated, err := r.client.UpdateIdentifier(ctx, id, saveRequest)
	if err != nil {
		resp.Diagnostics.Append(identifierErrorDiagnostic(fmt.Sprintf("Unable to update the Immuta identifier %s", id), err))

		return
	}

	plan.ID = state.ID

	resp.Diagnostics.Append(plan.applyIdentifier(ctx, updated)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *identifierResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state identifierResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	if err := r.client.DeleteIdentifier(ctx, id); err != nil {
		if immutaapi.IsNotFound(err) {
			// Already gone remotely, nothing left to do.
			return
		}

		resp.Diagnostics.Append(identifierErrorDiagnostic(fmt.Sprintf("Unable to delete the Immuta identifier %s", id), err))
	}
}

func (r *identifierResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// saveRequest builds the documented create and update body.
func (m *identifierResourceModel) saveRequest(ctx context.Context) (immutaapi.SaveIdentifierRequest, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	tags, diags := stringSlice(ctx, m.Tags)
	diagnostics.Append(diags...)

	values, diags := stringSlice(ctx, m.Values)
	diagnostics.Append(diags...)

	if diagnostics.HasError() {
		return immutaapi.SaveIdentifierRequest{}, diagnostics
	}

	config := immutaapi.IdentifierConfig{}
	if m.Type.ValueString() == immutaapi.IdentifierTypeDictionary {
		caseSensitive := m.CaseSensitive.ValueBool()
		config.Values = values
		config.CaseSensitive = &caseSensitive
	} else {
		config.Regex = m.Regex.ValueString()
	}

	request := immutaapi.NewSaveIdentifierRequest(
		m.Name.ValueString(),
		m.Description.ValueString(),
		m.Type.ValueString(),
		config,
		tags,
	)

	if isSet(m.DomainID) {
		domainID := m.DomainID.ValueString()
		request.DomainID = &domainID
	}

	return request, diagnostics
}

// applyIdentifier copies an API identifier onto the Terraform model.
func (m *identifierResourceModel) applyIdentifier(ctx context.Context, identifier *immutaapi.Identifier) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	m.ID = types.StringValue(identifier.ID)
	m.Name = types.StringValue(identifier.Name)
	m.Description = types.StringValue(identifier.Description)
	m.Type = types.StringValue(identifier.Type)
	m.DomainID = normalizeOptionalString(identifier.DomainID(), m.DomainID)
	m.CreatedAt = optionalComputedString(identifier.CreatedAt)
	m.UpdatedAt = optionalComputedString(identifier.UpdatedAt)

	tags, diags := stringSetValue(ctx, identifier.TagNames())
	diagnostics.Append(diags...)
	if diagnostics.HasError() {
		return diagnostics
	}
	m.Tags = tags

	if identifier.Type == immutaapi.IdentifierTypeDictionary {
		values, diags := stringSetValue(ctx, identifier.Config.Values)
		diagnostics.Append(diags...)
		if diagnostics.HasError() {
			return diagnostics
		}

		m.Values = values
		m.Regex = types.StringNull()
		m.CaseSensitive = types.BoolValue(identifier.Config.CaseSensitive != nil && *identifier.Config.CaseSensitive)

		return diagnostics
	}

	m.Regex = normalizeOptionalString(identifier.Config.Regex, m.Regex)
	m.Values = types.SetNull(types.StringType)
	m.CaseSensitive = types.BoolValue(false)

	return diagnostics
}
