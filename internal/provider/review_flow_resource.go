package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ resource.Resource                   = (*reviewFlowResource)(nil)
	_ resource.ResourceWithConfigure      = (*reviewFlowResource)(nil)
	_ resource.ResourceWithImportState    = (*reviewFlowResource)(nil)
	_ resource.ResourceWithValidateConfig = (*reviewFlowResource)(nil)
)

// reviewFlowTarget is what a review flow resource attaches flows to: an
// asset or a data product.
type reviewFlowTarget struct {
	name        string // resource type name suffix
	what        string // in messages
	idAttribute string
	idMarkdown  string
	description string
	put         func(c *immutaapi.Client, ctx context.Context, id, requestType string, graph map[string]any) error
	get         func(c *immutaapi.Client, ctx context.Context, id, requestType string) (*immutaapi.ReviewFlow, error)
	// remove removes the flow on destroy; nil when the API cannot, and
	// destroy only forgets it.
	remove func(c *immutaapi.Client, ctx context.Context, id, requestType string) error
}

const reviewFlowDocumentMarkdown = "The review flow graph, as a YAML or JSON document holding `nodes` and `edges`, usually read " +
	"from a file with `file()` or `templatefile()`. The root node id is always `" + reviewFlowRootNodeID + "` and the " +
	"request form node id `" + reviewFlowFormNodeID + "`; other nodes need their own UUIDs. Formatting, comments and " +
	"key order never cause a diff, and neither do fields Immuta adds, such as reviewer display names."

// NewAssetReviewFlowResource returns the immuta_asset_review_flow resource.
func NewAssetReviewFlowResource() resource.Resource {
	return &reviewFlowResource{target: reviewFlowTarget{
		name:        "asset_review_flow",
		what:        "asset",
		idAttribute: "asset_id",
		idMarkdown: "ID of the asset, the Immuta data object ID, for example `data.immuta_data_object.<name>.id` or " +
			"`immuta_custom_data_object.<name>.id`. Changing it replaces the review flow.",
		description: "Overrides the dynamic review flow of an asset for one request type, with the Request app API " +
			"(`PUT /api/asset/{id}/review-flows/{requestType}`). The asset's descendants inherit it unless they have " +
			"their own. Destroying it removes the override, and the asset inherits the nearest review flow again.\n\n" +
			"Dynamic review flows must be enabled on the tenant.",
		put:    (*immutaapi.Client).PutAssetReviewFlow,
		get:    (*immutaapi.Client).GetAssetReviewFlow,
		remove: (*immutaapi.Client).DeleteAssetReviewFlow,
	}}
}

// NewDataProductReviewFlowResource returns the
// immuta_data_product_review_flow resource.
func NewDataProductReviewFlowResource() resource.Resource {
	return &reviewFlowResource{target: reviewFlowTarget{
		name:        "data_product_review_flow",
		what:        "data product",
		idAttribute: "data_product_id",
		idMarkdown:  "ID of the data product, usually `immuta_data_product.<name>.id`. Changing it replaces the review flow.",
		description: "Sets the dynamic review flow of a data product for one request type, with the Request app API " +
			"(`PUT /api/data-product/{id}/review-flows/{requestType}`). It needs the GOVERNANCE permission or Manage " +
			"Data Products in the data product's domain.\n\n" +
			"A data product always has a review flow and the API cannot remove one, so destroying this resource only " +
			"removes it from the Terraform state; the last review flow stays on the data product.\n\n" +
			"Dynamic review flows must be enabled on the tenant.",
		put: (*immutaapi.Client).PutDataProductReviewFlow,
		get: (*immutaapi.Client).GetDataProductReviewFlow,
	}}
}

type reviewFlowResource struct {
	client *immutaapi.Client
	target reviewFlowTarget
}

// reviewFlowModel holds the attributes. The target ID attribute is named
// after the target, so the model is read and written attribute by attribute.
type reviewFlowModel struct {
	ID          types.String
	TargetID    types.String
	RequestType types.String
	ReviewFlow  yamlDocumentValue
}

type attributeGetter interface {
	GetAttribute(ctx context.Context, p path.Path, target any) diag.Diagnostics
}

func (r *reviewFlowResource) getModel(ctx context.Context, source attributeGetter) (reviewFlowModel, diag.Diagnostics) {
	var model reviewFlowModel
	var diagnostics diag.Diagnostics

	diagnostics.Append(source.GetAttribute(ctx, path.Root("id"), &model.ID)...)
	diagnostics.Append(source.GetAttribute(ctx, path.Root(r.target.idAttribute), &model.TargetID)...)
	diagnostics.Append(source.GetAttribute(ctx, path.Root("request_type"), &model.RequestType)...)
	diagnostics.Append(source.GetAttribute(ctx, path.Root("review_flow"), &model.ReviewFlow)...)

	return model, diagnostics
}

func (r *reviewFlowResource) setModel(ctx context.Context, state *tfsdk.State, model reviewFlowModel) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	diagnostics.Append(state.SetAttribute(ctx, path.Root("id"), model.ID)...)
	diagnostics.Append(state.SetAttribute(ctx, path.Root(r.target.idAttribute), model.TargetID)...)
	diagnostics.Append(state.SetAttribute(ctx, path.Root("request_type"), model.RequestType)...)
	diagnostics.Append(state.SetAttribute(ctx, path.Root("review_flow"), model.ReviewFlow)...)

	return diagnostics
}

func (r *reviewFlowResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.target.name
}

func (r *reviewFlowResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: r.target.description,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: fmt.Sprintf("`<%s>/<request_type>`.", r.target.idAttribute),
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			r.target.idAttribute: schema.StringAttribute{
				Required:            true,
				MarkdownDescription: r.target.idMarkdown,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers:       replace,
			},
			"request_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: fmt.Sprintf("The request type the review flow handles: %s. Changing it replaces the "+
					"review flow.", codeList(immutaapi.ReviewFlowRequestTypes)),
				Validators:    []validator.String{stringvalidator.OneOf(immutaapi.ReviewFlowRequestTypes...)},
				PlanModifiers: replace,
			},
			"review_flow": schema.StringAttribute{
				CustomType:          yamlDocumentType{},
				Required:            true,
				MarkdownDescription: reviewFlowDocumentMarkdown,
				PlanModifiers:       []planmodifier.String{keepEquivalentYAMLDocument{}},
			},
		},
	}
}

func (r *reviewFlowResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
	requireRequestApp(r.client, "immuta_"+r.target.name, &resp.Diagnostics)
}

// ValidateConfig checks the structure of the graph at plan time.
func (r *reviewFlowResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var document yamlDocumentValue

	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("review_flow"), &document)...)
	if resp.Diagnostics.HasError() || document.IsNull() || document.IsUnknown() {
		return
	}
	if _, err := parseReviewFlow(document.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("review_flow"), "Invalid review flow", err.Error())
	}
}

func (r *reviewFlowResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	plan, diags := r.getModel(ctx, req.Plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.save(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = types.StringValue(reviewFlowID(plan.TargetID.ValueString(), plan.RequestType.ValueString()))
	resp.Diagnostics.Append(r.setModel(ctx, &resp.State, plan)...)
}

func (r *reviewFlowResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	state, diags := r.getModel(ctx, req.State)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	targetID, requestType := state.TargetID.ValueString(), state.RequestType.ValueString()

	flow, err := r.target.get(r.client, ctx, targetID, requestType)
	if err != nil && !immutaapi.IsNotFound(err) {
		resp.Diagnostics.Append(reviewFlowErrorDiagnostic(
			fmt.Sprintf("Unable to read the %s review flow of the %s %s", requestType, r.target.what, targetID), err))

		return
	}
	// An asset that inherits its review flow no longer has its own.
	if flow == nil || flow.InheritedFrom != "" {
		tflog.Debug(ctx, "The Immuta review flow no longer exists, removing it from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)

		return
	}

	next := remoteReviewFlow(flow.Graph)
	if !state.ReviewFlow.IsNull() {
		configured, err := parseYAMLMapping(state.ReviewFlow.ValueString(), "review_flow")
		if err == nil {
			next = projectReviewFlow(configured, flow.Graph)
			if equalJSON(next, configured) {
				next = nil
			}
		}
	}
	if next != nil {
		encoded, err := encodeJSONDocument(next)
		if err != nil {
			resp.Diagnostics.AddError("Unable to record the Immuta review flow", err.Error())

			return
		}
		state.ReviewFlow = yamlDocumentValue{StringValue: types.StringValue(encoded)}
	}

	state.ID = types.StringValue(reviewFlowID(targetID, requestType))
	resp.Diagnostics.Append(r.setModel(ctx, &resp.State, state)...)
}

// Update saves the new graph: every other configurable attribute requires
// replacement.
func (r *reviewFlowResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	plan, diags := r.getModel(ctx, req.Plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.save(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = types.StringValue(reviewFlowID(plan.TargetID.ValueString(), plan.RequestType.ValueString()))
	resp.Diagnostics.Append(r.setModel(ctx, &resp.State, plan)...)
}

func (r *reviewFlowResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	state, diags := r.getModel(ctx, req.State)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	targetID, requestType := state.TargetID.ValueString(), state.RequestType.ValueString()

	if r.target.remove == nil {
		resp.Diagnostics.AddWarning("The Immuta review flow was only removed from the Terraform state",
			fmt.Sprintf("The Request app API cannot remove the review flow of a %s, so the %s review flow of %s stays "+
				"as it was last saved.", r.target.what, requestType, targetID))

		return
	}

	if err := r.target.remove(r.client, ctx, targetID, requestType); err != nil && !immutaapi.IsNotFound(err) {
		resp.Diagnostics.Append(reviewFlowErrorDiagnostic(
			fmt.Sprintf("Unable to remove the %s review flow of the %s %s", requestType, r.target.what, targetID), err))
	}
}

// ImportState imports a review flow by <target id>/<request type>.
func (r *reviewFlowResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	separator := strings.LastIndex(req.ID, "/")
	if separator <= 0 || !isReviewFlowRequestType(req.ID[separator+1:]) {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Expected <%s>/<request type>, with a request type of %s, got %q.",
				r.target.idAttribute, strings.Join(immutaapi.ReviewFlowRequestTypes, " or "), req.ID))

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(r.target.idAttribute), req.ID[:separator])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("request_type"), req.ID[separator+1:])...)
}

func (r *reviewFlowResource) save(ctx context.Context, model reviewFlowModel) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	graph, err := parseReviewFlow(model.ReviewFlow.ValueString())
	if err != nil {
		diagnostics.AddAttributeError(path.Root("review_flow"), "Invalid review flow", err.Error())

		return diagnostics
	}

	targetID, requestType := model.TargetID.ValueString(), model.RequestType.ValueString()
	if err := r.target.put(r.client, ctx, targetID, requestType, graph); err != nil {
		diagnostics.Append(reviewFlowErrorDiagnostic(
			fmt.Sprintf("Unable to save the %s review flow of the %s %s", requestType, r.target.what, targetID), err))

		return diagnostics
	}

	tflog.Debug(ctx, "Saved an Immuta review flow", map[string]any{"target": r.target.what, "id": targetID, "request_type": requestType})

	return diagnostics
}

func reviewFlowID(targetID, requestType string) string {
	return targetID + "/" + requestType
}

func isReviewFlowRequestType(value string) bool {
	for _, requestType := range immutaapi.ReviewFlowRequestTypes {
		if value == requestType {
			return true
		}
	}

	return false
}

// reviewFlowErrorDiagnostic explains the documented causes of a failed
// review flow request.
func reviewFlowErrorDiagnostic(summary string, err error) diag.Diagnostic {
	var apiErr *immutaapi.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusBadRequest:
			return diag.NewErrorDiagnostic(summary, err.Error()+"\n\nImmuta validates the graph structure, decision "+
				"expressions and reviewers before saving; its reason is shown above.")
		case http.StatusForbidden:
			return diag.NewErrorDiagnostic(summary, err.Error()+"\n\nSaving a review flow needs the GOVERNANCE "+
				"permission or, for a data product, Manage Data Products in its domain.")
		case http.StatusNotFound:
			return diag.NewErrorDiagnostic(summary, err.Error()+"\n\nCheck the ID, and that dynamic review flows are "+
				"enabled on the tenant.")
		}
	}

	return requestAppErrorDiagnostic(summary, err)
}
