package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ resource.Resource                   = (*globalPolicyResource)(nil)
	_ resource.ResourceWithConfigure      = (*globalPolicyResource)(nil)
	_ resource.ResourceWithImportState    = (*globalPolicyResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*globalPolicyResource)(nil)
	_ resource.ResourceWithValidateConfig = (*globalPolicyResource)(nil)
)

// NewGlobalPolicyResource returns the immuta_global_policy resource.
func NewGlobalPolicyResource() resource.Resource {
	return &globalPolicyResource{}
}

type globalPolicyResource struct {
	client *immutaapi.Client
}

// globalPolicyResourceModel is a global policy written with the V2 policy
// API. The policy is kept as the YAML or JSON document the user wrote.
type globalPolicyResourceModel struct {
	ID        types.String      `tfsdk:"id"`
	Policy    yamlDocumentValue `tfsdk:"policy"`
	PolicyKey types.String      `tfsdk:"policy_key"`
	Name      types.String      `tfsdk:"name"`
	Type      types.String      `tfsdk:"type"`
}

func (r *globalPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_global_policy"
}

func (r *globalPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an Immuta global data or subscription policy, written as a YAML or JSON document " +
			"with the V2 policy API (`POST /api/v2/policy`).\n\n" +
			"Changes to the document update the policy in place. Changing its `policyKey` or `type` replaces it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ID of the global policy, assigned by Immuta.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"policy": schema.StringAttribute{
				CustomType: yamlDocumentType{},
				Required:   true,
				MarkdownDescription: "The policy, as a YAML or JSON document in the Immuta V2 policy format, usually " +
					"read with `file()`. It needs `policyKey`, `name`, `type` (`data` or `subscription`) and `actions`. " +
					"Formatting, comments and key order do not produce a diff.",
				PlanModifiers: []planmodifier.String{
					keepEquivalentYAMLDocument{},
				},
			},
			"policy_key": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The `policyKey` that identifies the policy in Immuta.",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Name of the policy.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Type of the policy: `data` or `subscription`.",
			},
		},
	}
}

func (r *globalPolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

// ValidateConfig checks the document at plan time, before any request.
func (r *globalPolicyResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var policy yamlDocumentValue

	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("policy"), &policy)...)
	if resp.Diagnostics.HasError() || policy.IsNull() || policy.IsUnknown() {
		return
	}

	_, _, diags := readPolicyDocument(policy)
	resp.Diagnostics.Append(diags...)
}

// ModifyPlan shows the policy key, name and type from the document in the
// plan, and replaces the policy when its key or type changes. The V2 API
// identifies a policy by its key, so saving a document with another key
// would create a second policy and leave the first one behind.
func (r *globalPolicyResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}

	var plan globalPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.Policy.IsUnknown() || plan.Policy.IsNull() {
		return
	}

	_, fields, diags := readPolicyDocument(plan.Policy)
	if diags.HasError() {
		// Reported by ValidateConfig.
		return
	}

	plan.PolicyKey = types.StringValue(fields.PolicyKey)
	plan.Name = types.StringValue(fields.Name)
	plan.Type = types.StringValue(fields.Type)
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)

	if req.State.Raw.IsNull() {
		return
	}

	var state globalPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if state.PolicyKey.ValueString() != fields.PolicyKey || state.Type.ValueString() != fields.Type {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("policy"))
	}
}

func (r *globalPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan globalPolicyResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, diags := r.save(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Created an Immuta global policy", map[string]any{"id": id, "policy_key": plan.PolicyKey.ValueString()})

	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(r.recordFingerprint(ctx, id, resp.Private)...)
}

func (r *globalPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state globalPolicyResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	policy, err := r.client.GetGlobalPolicy(ctx, id)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			tflog.Debug(ctx, "Immuta global policy no longer exists, removing it from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.Append(globalPolicyErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta global policy %s", id), err))

		return
	}

	// The V1 read does not return the policy in the V2 document format, so
	// only the key, name and type are refreshed. A change to them in Immuta
	// shows up as a diff, because the plan takes them from the document.
	state.ID = types.StringValue(policy.ID)
	state.PolicyKey = types.StringValue(policy.PolicyKey)
	state.Name = types.StringValue(policy.Name)
	state.Type = types.StringValue(policy.Type)

	// A change to the rest of the policy is detected by comparing the
	// fingerprint of its V1 content with the one recorded when it was last
	// saved. On a change the document is cleared from state, so the plan
	// saves the configured document again.
	stored, diags := getGlobalPolicyFingerprint(ctx, req.Private)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	switch stored {
	case policy.Fingerprint:
	case "":
		// Nothing recorded, after an import, an upgrade from a version that
		// did not record it, or a failed read back. The content in Immuta may
		// already differ from the document, and taking it as the baseline
		// would hide that for good, so the document is saved again once; the
		// save records the baseline.
		tflog.Debug(ctx, "No recorded content for the Immuta global policy, planning to save the document again", map[string]any{"id": id})
		state.Policy = yamlDocumentValue{StringValue: types.StringNull()}
	default:
		tflog.Debug(ctx, "Immuta global policy was changed in Immuta, planning to save the document again", map[string]any{"id": id})
		state.Policy = yamlDocumentValue{StringValue: types.StringNull()}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *globalPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state globalPolicyResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, diags := r.save(ctx, plan, state.ID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(r.recordFingerprint(ctx, id, resp.Private)...)
}

func (r *globalPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state globalPolicyResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	if err := r.client.DeleteGlobalPolicy(ctx, id); err != nil && !immutaapi.IsNotFound(err) {
		resp.Diagnostics.Append(globalPolicyErrorDiagnostic(fmt.Sprintf("Unable to delete the Immuta global policy %s", id), err))
	}
}

// ImportState imports a global policy by its id. The policy document cannot
// be read back in the V2 format, so the first apply after an import saves the
// configured document. A document with another policyKey replaces the policy.
// That save records the content fingerprint used to detect changes.
func (r *globalPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// globalPolicyFingerprintKey is the private state key holding the
// fingerprint of the policy content as last saved or first read.
const globalPolicyFingerprintKey = "policy_fingerprint"

// privateState is the part of the framework private state used here.
type privateState interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

func getGlobalPolicyFingerprint(ctx context.Context, private privateState) (string, diag.Diagnostics) {
	value, diags := private.GetKey(ctx, globalPolicyFingerprintKey)
	if diags.HasError() || len(value) == 0 {
		return "", diags
	}

	var fingerprint string
	if err := json.Unmarshal(value, &fingerprint); err != nil {
		// Unreadable private state is treated as nothing recorded.
		return "", diags
	}

	return fingerprint, diags
}

// setGlobalPolicyFingerprint records a fingerprint; an empty one clears it.
func setGlobalPolicyFingerprint(ctx context.Context, private privateState, fingerprint string) diag.Diagnostics {
	if fingerprint == "" {
		return private.SetKey(ctx, globalPolicyFingerprintKey, nil)
	}

	value, err := json.Marshal(fingerprint)
	if err != nil {
		var diags diag.Diagnostics
		diags.AddError("Unable to record the Immuta global policy content", err.Error())

		return diags
	}

	return private.SetKey(ctx, globalPolicyFingerprintKey, value)
}

// recordFingerprint reads a saved policy back and records the fingerprint of
// its content. A failed read is only a warning, as the policy was saved: the
// recorded fingerprint is cleared, so the next plan saves the document again.
func (r *globalPolicyResource) recordFingerprint(ctx context.Context, id string, private privateState) diag.Diagnostics {
	policy, err := r.client.GetGlobalPolicy(ctx, id)
	if err != nil {
		diags := setGlobalPolicyFingerprint(ctx, private, "")
		diags.AddWarning(
			"Unable to record the Immuta global policy content",
			fmt.Sprintf("The policy %s was saved, but reading it back failed: %s. The next apply saves the document again to record it.", id, err),
		)

		return diags
	}

	return setGlobalPolicyFingerprint(ctx, private, policy.Fingerprint)
}

// save posts the planned document and returns the id of the saved policy.
func (r *globalPolicyResource) save(ctx context.Context, plan globalPolicyResourceModel, knownIDs ...string) (string, diag.Diagnostics) {
	document, fields, diags := readPolicyDocument(plan.Policy)
	if diags.HasError() {
		return "", diags
	}

	response, err := r.client.SaveGlobalPolicy(ctx, document)
	if err != nil {
		diags.Append(globalPolicyErrorDiagnostic(fmt.Sprintf("Unable to save the Immuta global policy %q", fields.PolicyKey), err))

		return "", diags
	}

	id, err := r.client.ResolveGlobalPolicyID(ctx, response, fields.PolicyKey, fields.Name, fields.Type, knownIDs...)
	if err != nil {
		diags.AddError(
			fmt.Sprintf("Unable to find the saved Immuta global policy %q", fields.PolicyKey),
			err.Error()+"\n\nImmuta accepted the policy, so it exists but is not recorded in Terraform state. "+
				"Import it with its global policy id, or delete it in Immuta, before applying again.",
		)

		return "", diags
	}

	return id, diags
}

// readPolicyDocument parses and checks a policy document, returning
// warnings for undocumented top-level keys.
func readPolicyDocument(policy yamlDocumentValue) (map[string]any, policyDocumentFields, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	document, err := parsePolicyDocument(policy.ValueString())
	if err != nil {
		diagnostics.AddAttributeError(path.Root("policy"), "Invalid policy document", err.Error())

		return nil, policyDocumentFields{}, diagnostics
	}

	fields, warnings, err := checkPolicyDocument(document)
	if err != nil {
		diagnostics.AddAttributeError(path.Root("policy"), "Invalid policy document", err.Error())

		return nil, fields, diagnostics
	}

	for _, warning := range warnings {
		diagnostics.AddAttributeWarning(path.Root("policy"), "Undocumented policy key", warning)
	}

	return document, fields, diagnostics
}

// globalPolicyErrorDiagnostic turns an error from the policy endpoints into
// an actionable diagnostic. API errors never contain credentials or full
// response payloads.
func globalPolicyErrorDiagnostic(summary string, err error) diag.Diagnostic {
	detail := err.Error()

	var apiErr *immutaapi.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			detail += "\n\nCheck that the configured API key is valid and that the Immuta user holds the GOVERNANCE " +
				"permission needed to manage global policies."
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			detail += "\n\nImmuta rejected the policy document. Check it against the Immuta V2 policy reference; " +
				"Immuta's reason is shown above."
		}
	}

	return diag.NewErrorDiagnostic(summary, detail)
}
