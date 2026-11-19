package provider

import (
	"context"
	"fmt"
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
	_ resource.Resource                = (*domainIdentifierResource)(nil)
	_ resource.ResourceWithConfigure   = (*domainIdentifierResource)(nil)
	_ resource.ResourceWithImportState = (*domainIdentifierResource)(nil)
)

// NewDomainIdentifierResource returns the immuta_domain_identifier resource.
func NewDomainIdentifierResource() resource.Resource {
	return &domainIdentifierResource{}
}

type domainIdentifierResource struct {
	client *immutaapi.Client
}

// domainIdentifierResourceModel is the Terraform representation of a
// reference identifier that has been copied into a domain.
type domainIdentifierResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	DomainID           types.String `tfsdk:"domain_id"`
	SourceIdentifierID types.String `tfsdk:"source_identifier_id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	Type               types.String `tfsdk:"type"`
	Tags               types.Set    `tfsdk:"tags"`
	CreatedAt          types.String `tfsdk:"created_at"`
	UpdatedAt          types.String `tfsdk:"updated_at"`
}

func (r *domainIdentifierResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_identifier"
}

func (r *domainIdentifierResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Copies an existing identifier into an Immuta domain with the documented " +
			"`/sdd/identifier/bulk-copy/{domainId}` API.\n\n" +
			"~> **Immuta copies, it does not move.** The copy is a brand new identifier with its own ID that lives in the " +
			"domain, and the identifier it was copied from is left untouched. Editing the copy afterwards does not change " +
			"the original, and editing the original does not change the copy.\n\n" +
			"Use this to put a tenant-wide reference identifier to work inside a domain. To author a new identifier directly " +
			"in a domain, set `domain_id` on `immuta_identifier` instead.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ID of the copy that Immuta created inside the domain.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"domain_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of the domain to copy the identifier into, for example `immuta_domain.finance.id`. Changing it makes a new copy.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"source_identifier_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of the identifier to copy, for example `immuta_identifier.credit_card.id`. Changing it makes a new copy.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Name of the copy, taken from the identifier it was copied from.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the copy.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Criteria type of the copy.",
			},
			"tags": schema.SetAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Fully qualified names of the tags the copy applies.",
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timestamp of when the copy was created.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timestamp of when the copy was last updated.",
			},
		},
	}
}

func (r *domainIdentifierResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *domainIdentifierResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainIdentifierResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domainID := plan.DomainID.ValueString()
	sourceID := plan.SourceIdentifierID.ValueString()

	copied, err := r.copyIdentifier(ctx, domainID, sourceID)
	if err != nil {
		resp.Diagnostics.Append(identifierErrorDiagnostic(
			fmt.Sprintf("Unable to copy the Immuta identifier %s into the domain %s", sourceID, domainID),
			err,
		))

		return
	}

	tflog.Debug(ctx, "Copied an Immuta identifier into a domain", map[string]any{
		"domain_id": domainID,
		"source_id": sourceID,
		"copy_id":   copied.ID,
	})

	resp.Diagnostics.Append(plan.applyIdentifier(ctx, copied)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// copyIdentifier copies one identifier into a domain and returns the copy.
func (r *domainIdentifierResource) copyIdentifier(ctx context.Context, domainID, sourceID string) (*immutaapi.Identifier, error) {
	copies, err := r.client.CopyIdentifiersToDomain(ctx, domainID, []string{sourceID})
	if err != nil {
		return nil, err
	}

	return &copies[0], nil
}

func (r *domainIdentifierResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainIdentifierResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	identifier, err := r.client.GetIdentifier(ctx, id)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			tflog.Debug(ctx, "Copied Immuta identifier no longer exists, removing it from state", map[string]any{"id": id})
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

// Update exists to satisfy the resource interface. Both configurable
// attributes require replacement, so there is nothing to send.
func (r *domainIdentifierResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state domainIdentifierResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = state.ID

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *domainIdentifierResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state domainIdentifierResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only the copy is deleted. The identifier it was copied from is left
	// alone, which is what copy semantics imply.
	id := state.ID.ValueString()

	if err := r.client.DeleteIdentifier(ctx, id); err != nil {
		if immutaapi.IsNotFound(err) {
			// Already gone remotely, nothing left to do.
			return
		}

		resp.Diagnostics.Append(identifierErrorDiagnostic(fmt.Sprintf("Unable to delete the copied Immuta identifier %s", id), err))
	}
}

// ImportState accepts "{source_identifier_id}/{copy_id}". The identifier a
// copy came from is not recorded by Immuta, so it has to be supplied.
func (r *domainIdentifierResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(strings.TrimSpace(req.ID), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID for a copied Immuta identifier",
			fmt.Sprintf("Import a copied identifier as \"{source_identifier_id}/{copy_id}\". Immuta does not record which identifier a copy came from, so both are needed. Got %q.", req.ID),
		)

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("source_identifier_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

// applyIdentifier copies an API identifier onto the Terraform model.
func (m *domainIdentifierResourceModel) applyIdentifier(ctx context.Context, identifier *immutaapi.Identifier) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	m.ID = types.StringValue(identifier.ID)
	m.Name = types.StringValue(identifier.Name)
	m.Description = types.StringValue(identifier.Description)
	m.Type = types.StringValue(identifier.Type)
	m.CreatedAt = optionalComputedString(identifier.CreatedAt)
	m.UpdatedAt = optionalComputedString(identifier.UpdatedAt)

	if domainID := identifier.DomainID(); domainID != "" {
		m.DomainID = types.StringValue(domainID)
	}

	tags, diags := stringSetValue(ctx, identifier.TagNames())
	diagnostics.Append(diags...)
	if diagnostics.HasError() {
		return diagnostics
	}
	m.Tags = tags

	return diagnostics
}
