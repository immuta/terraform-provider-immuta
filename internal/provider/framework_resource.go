package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ resource.Resource                = (*frameworkResource)(nil)
	_ resource.ResourceWithConfigure   = (*frameworkResource)(nil)
	_ resource.ResourceWithImportState = (*frameworkResource)(nil)
)

// NewFrameworkResource returns the immuta_framework resource.
func NewFrameworkResource() resource.Resource {
	return &frameworkResource{}
}

type frameworkResource struct {
	client *immutaapi.Client
}

// frameworkResourceModel is the Terraform representation of a framework.
//
// tags, rules and every list inside them are sets, so the order Immuta
// returns them in never produces a diff.
type frameworkResourceModel struct {
	ID          types.String `tfsdk:"id"`
	ShortName   types.String `tfsdk:"short_name"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Active      types.Bool   `tfsdk:"active"`
	Tags        types.Set    `tfsdk:"tags"`
	Rules       types.Set    `tfsdk:"rules"`
	Version     types.String `tfsdk:"version"`
	CreatedBy   types.Int64  `tfsdk:"created_by"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

type frameworkTagModel struct {
	Name          types.String `tfsdk:"name"`
	Source        types.String `tfsdk:"source"`
	Sensitivities types.Set    `tfsdk:"sensitivities"`
}

type frameworkSensitivityModel struct {
	Dimension   types.String `tfsdk:"dimension"`
	Sensitivity types.Int64  `tfsdk:"sensitivity"`
}

type frameworkTagRefModel struct {
	Name   types.String `tfsdk:"name"`
	Source types.String `tfsdk:"source"`
}

type frameworkRuleModel struct {
	Name               types.String         `tfsdk:"name"`
	ClassificationTag  frameworkTagRefModel `tfsdk:"classification_tag"`
	ColumnTags         types.Set            `tfsdk:"column_tags"`
	NeighborColumnTags types.Set            `tfsdk:"neighbor_column_tags"`
	TableTags          types.Set            `tfsdk:"table_tags"`
}

var (
	frameworkTagRefType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"name":   types.StringType,
		"source": types.StringType,
	}}
	frameworkSensitivityType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"dimension":   types.StringType,
		"sensitivity": types.Int64Type,
	}}
	frameworkTagType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"name":          types.StringType,
		"source":        types.StringType,
		"sensitivities": types.SetType{ElemType: frameworkSensitivityType},
	}}
	frameworkRuleType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"name":                 types.StringType,
		"classification_tag":   frameworkTagRefType,
		"column_tags":          types.SetType{ElemType: frameworkTagRefType},
		"neighbor_column_tags": types.SetType{ElemType: frameworkTagRefType},
		"table_tags":           types.SetType{ElemType: frameworkTagRefType},
	}}
)

func (r *frameworkResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_framework"
}

// tagRefAttributes are the attributes of a tag referenced by name and source.
func tagRefAttributes(what string) map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"name": schema.StringAttribute{
			Required:            true,
			MarkdownDescription: fmt.Sprintf("Fully qualified name of the %s, including any parent tags.", what),
			Validators: []validator.String{
				stringvalidator.LengthAtLeast(1),
			},
		},
		"source": schema.StringAttribute{
			Required: true,
			MarkdownDescription: fmt.Sprintf("Catalog the %s is from. `curated` is any tag in Immuta; "+
				"otherwise use the external catalog ID.", what),
			Validators: []validator.String{
				stringvalidator.LengthAtLeast(1),
			},
		},
	}
}

// criteriaAttribute is one of the optional tag criteria of a rule. An empty
// set is rejected so that "no criteria" always means null, which is how an
// empty list from Immuta is stored.
func criteriaAttribute(description string) schema.SetNestedAttribute {
	return schema.SetNestedAttribute{
		Optional:            true,
		MarkdownDescription: description,
		NestedObject: schema.NestedAttributeObject{
			Attributes: tagRefAttributes("criteria tag"),
		},
		Validators: []validator.Set{
			setvalidator.SizeAtLeast(1),
		},
	}
}

func (r *frameworkResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an Immuta classification framework with the documented `/frameworks` API.\n\n" +
			"A framework applies classification tags to columns based on the tags already found on the column, " +
			"its neighbouring columns or its data source. System-created frameworks cannot be edited, so manage " +
			"your own framework rather than importing a system one.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Unique ID of the framework, assigned by Immuta.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"short_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Short, human-readable name of the framework.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Official, human-readable name of the framework.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"description": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Description of the framework. Immuta requires one.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"active": schema.BoolAttribute{
				Required:            true,
				MarkdownDescription: "Whether Immuta actively uses the framework on data sources.",
			},
			"tags": schema.SetNestedAttribute{
				Required: true,
				MarkdownDescription: "Tags used in the framework, and the sensitivity attached to them. " +
					"Each tag the framework uses must have an entry.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name":   tagRefAttributes("tag")["name"],
						"source": tagRefAttributes("tag")["source"],
						"sensitivities": schema.SetNestedAttribute{
							Optional: true,
							MarkdownDescription: "Sensitivity assigned to the tag, which drives the audit dashboards and " +
								"monitors. Omit for no sensitivity.",
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"dimension": schema.StringAttribute{
										Required:            true,
										MarkdownDescription: "Type of sensitivity. The only documented value is `confidentiality`.",
										Validators: []validator.String{
											stringvalidator.OneOf(immutaapi.SensitivityDimensionConfidentiality),
										},
									},
									"sensitivity": schema.Int64Attribute{
										Required: true,
										MarkdownDescription: "`0` for non-sensitive, `1` for sensitive and `2` for " +
											"highly-sensitive data.",
										Validators: []validator.Int64{
											int64validator.Between(immutaapi.SensitivityNonSensitive, immutaapi.SensitivityHighlySensitive),
										},
									},
								},
							},
							Validators: []validator.Set{
								setvalidator.SizeAtLeast(1),
							},
						},
					},
				},
			},
			"rules": schema.SetNestedAttribute{
				Required: true,
				MarkdownDescription: fmt.Sprintf("Rules that apply the framework's tags. A framework can have at most %d rules.",
					immutaapi.FrameworkMaxRules),
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Short, human-readable name of the rule.",
							Validators: []validator.String{
								stringvalidator.LengthAtLeast(1),
							},
						},
						"classification_tag": schema.SingleNestedAttribute{
							Required:            true,
							MarkdownDescription: "Tag to apply when the criteria match.",
							Attributes:          tagRefAttributes("classification tag"),
						},
						"column_tags": criteriaAttribute("Apply the classification tag to a column when one of these " +
							"tags is found on the same column."),
						"neighbor_column_tags": criteriaAttribute("Apply the classification tag to every column in a " +
							"data source when one of these tags is found on any of its columns."),
						"table_tags": criteriaAttribute("Apply the classification tag to every column in a data source " +
							"when one of these tags is found on the data source."),
					},
				},
				Validators: []validator.Set{
					setvalidator.SizeAtMost(immutaapi.FrameworkMaxRules),
				},
			},
			"version": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "ID of the current version of the framework, assigned by Immuta. It changes " +
					"every time the framework is updated.",
			},
			"created_by": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "ID of the user who created the framework. Null when Immuta does not return a numeric user ID.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timestamp of when the framework was created.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *frameworkResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *frameworkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan frameworkResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	saveRequest, diags := plan.saveRequest(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateFramework(ctx, saveRequest)
	if err != nil {
		resp.Diagnostics.Append(frameworkErrorDiagnostic("Unable to create the Immuta framework", err))

		return
	}

	tflog.Debug(ctx, "Created an Immuta framework", map[string]any{"id": created.ID})

	resp.Diagnostics.Append(plan.applyFramework(ctx, created)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *frameworkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state frameworkResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	framework, err := r.client.GetFramework(ctx, id)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			tflog.Debug(ctx, "Immuta framework no longer exists, removing it from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.Append(frameworkErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta framework %s", id), err))

		return
	}

	resp.Diagnostics.Append(state.applyFramework(ctx, framework)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *frameworkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state frameworkResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// The complete framework is sent, so that what Immuta stores matches the
	// configuration even when it has drifted in attributes that did not change.
	saveRequest, diags := plan.saveRequest(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updated, err := r.client.UpdateFramework(ctx, id, saveRequest)
	if err != nil {
		resp.Diagnostics.Append(frameworkErrorDiagnostic(fmt.Sprintf("Unable to update the Immuta framework %s", id), err))

		return
	}

	plan.ID = state.ID

	resp.Diagnostics.Append(plan.applyFramework(ctx, updated)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *frameworkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state frameworkResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	if err := r.client.DeleteFramework(ctx, id); err != nil {
		if immutaapi.IsNotFound(err) {
			// Already gone remotely, nothing left to do.
			return
		}

		resp.Diagnostics.Append(frameworkErrorDiagnostic(fmt.Sprintf("Unable to delete the Immuta framework %s", id), err))
	}
}

func (r *frameworkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// saveRequest builds the documented framework payload from the plan.
func (m *frameworkResourceModel) saveRequest(ctx context.Context) (immutaapi.SaveFrameworkRequest, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	request := immutaapi.SaveFrameworkRequest{
		ShortName:   m.ShortName.ValueString(),
		Name:        m.Name.ValueString(),
		Description: m.Description.ValueString(),
		Active:      m.Active.ValueBool(),
	}

	var tags []frameworkTagModel
	diagnostics.Append(m.Tags.ElementsAs(ctx, &tags, false)...)

	var rules []frameworkRuleModel
	diagnostics.Append(m.Rules.ElementsAs(ctx, &rules, false)...)

	if diagnostics.HasError() {
		return request, diagnostics
	}

	for _, tag := range tags {
		var sensitivities []frameworkSensitivityModel
		if !tag.Sensitivities.IsNull() {
			diagnostics.Append(tag.Sensitivities.ElementsAs(ctx, &sensitivities, false)...)
		}

		apiTag := immutaapi.FrameworkTag{Name: tag.Name.ValueString(), Source: tag.Source.ValueString()}
		for _, sensitivity := range sensitivities {
			apiTag.Sensitivities = append(apiTag.Sensitivities, immutaapi.FrameworkSensitivity{
				Dimension:   sensitivity.Dimension.ValueString(),
				Sensitivity: sensitivity.Sensitivity.ValueInt64(),
			})
		}
		request.Tags = append(request.Tags, apiTag)
	}

	for _, rule := range rules {
		columnTags, diags := tagRefsFromSet(ctx, rule.ColumnTags)
		diagnostics.Append(diags...)
		neighborColumnTags, diags := tagRefsFromSet(ctx, rule.NeighborColumnTags)
		diagnostics.Append(diags...)
		tableTags, diags := tagRefsFromSet(ctx, rule.TableTags)
		diagnostics.Append(diags...)

		request.Rules = append(request.Rules, immutaapi.FrameworkRule{
			Name: rule.Name.ValueString(),
			ClassificationTag: immutaapi.FrameworkTagRef{
				Name:   rule.ClassificationTag.Name.ValueString(),
				Source: rule.ClassificationTag.Source.ValueString(),
			},
			ColumnTags:         columnTags,
			NeighborColumnTags: neighborColumnTags,
			TableTags:          tableTags,
		})
	}

	return request, diagnostics
}

func tagRefsFromSet(ctx context.Context, set types.Set) ([]immutaapi.FrameworkTagRef, diag.Diagnostics) {
	if set.IsNull() || set.IsUnknown() {
		return nil, nil
	}

	var models []frameworkTagRefModel
	diagnostics := set.ElementsAs(ctx, &models, false)

	refs := make([]immutaapi.FrameworkTagRef, 0, len(models))
	for _, model := range models {
		refs = append(refs, immutaapi.FrameworkTagRef{Name: model.Name.ValueString(), Source: model.Source.ValueString()})
	}

	return refs, diagnostics
}

// applyFramework copies an API framework onto the Terraform model.
//
// Empty optional lists are stored as null. The schema rejects an empty set
// for those attributes, so null is the only way to configure "none" and the
// two can never disagree.
func (m *frameworkResourceModel) applyFramework(ctx context.Context, framework *immutaapi.Framework) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	m.ID = types.StringValue(framework.ID)
	m.ShortName = types.StringValue(framework.ShortName)
	m.Name = types.StringValue(framework.Name)
	m.Description = types.StringValue(framework.Description)
	m.Active = types.BoolValue(framework.Active)
	m.Version = optionalComputedString(framework.Version)
	m.CreatedAt = optionalComputedString(framework.CreatedAt)

	if framework.CreatedBy != nil {
		m.CreatedBy = types.Int64Value(*framework.CreatedBy)
	} else {
		m.CreatedBy = types.Int64Null()
	}

	tags := make([]attr.Value, 0, len(framework.Tags))
	for _, tag := range framework.Tags {
		sensitivities := make([]attr.Value, 0, len(tag.Sensitivities))
		for _, sensitivity := range tag.Sensitivities {
			value, diags := types.ObjectValue(frameworkSensitivityType.AttrTypes, map[string]attr.Value{
				"dimension":   types.StringValue(sensitivity.Dimension),
				"sensitivity": types.Int64Value(sensitivity.Sensitivity),
			})
			diagnostics.Append(diags...)
			sensitivities = append(sensitivities, value)
		}

		sensitivitySet, diags := optionalSet(frameworkSensitivityType, sensitivities)
		diagnostics.Append(diags...)

		value, diags := types.ObjectValue(frameworkTagType.AttrTypes, map[string]attr.Value{
			"name":          types.StringValue(tag.Name),
			"source":        types.StringValue(tag.Source),
			"sensitivities": sensitivitySet,
		})
		diagnostics.Append(diags...)
		tags = append(tags, value)
	}

	rules := make([]attr.Value, 0, len(framework.Rules))
	for _, rule := range framework.Rules {
		value, diags := frameworkRuleValue(rule)
		diagnostics.Append(diags...)
		rules = append(rules, value)
	}

	if diagnostics.HasError() {
		return diagnostics
	}

	tagSet, diags := types.SetValue(frameworkTagType, tags)
	diagnostics.Append(diags...)
	ruleSet, diags := types.SetValue(frameworkRuleType, rules)
	diagnostics.Append(diags...)

	m.Tags = tagSet
	m.Rules = ruleSet

	return diagnostics
}

// frameworkRuleValue converts an API rule into a rules set element. Empty
// criteria are stored as null.
func frameworkRuleValue(rule immutaapi.FrameworkRule) (types.Object, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	classificationTag, diags := tagRefValue(rule.ClassificationTag)
	diagnostics.Append(diags...)
	columnTags, diags := tagRefSet(rule.ColumnTags)
	diagnostics.Append(diags...)
	neighborColumnTags, diags := tagRefSet(rule.NeighborColumnTags)
	diagnostics.Append(diags...)
	tableTags, diags := tagRefSet(rule.TableTags)
	diagnostics.Append(diags...)

	if diagnostics.HasError() {
		return types.ObjectNull(frameworkRuleType.AttrTypes), diagnostics
	}

	value, diags := types.ObjectValue(frameworkRuleType.AttrTypes, map[string]attr.Value{
		"name":                 types.StringValue(rule.Name),
		"classification_tag":   classificationTag,
		"column_tags":          columnTags,
		"neighbor_column_tags": neighborColumnTags,
		"table_tags":           tableTags,
	})
	diagnostics.Append(diags...)

	return value, diagnostics
}

func tagRefValue(ref immutaapi.FrameworkTagRef) (types.Object, diag.Diagnostics) {
	return types.ObjectValue(frameworkTagRefType.AttrTypes, map[string]attr.Value{
		"name":   types.StringValue(ref.Name),
		"source": types.StringValue(ref.Source),
	})
}

func tagRefSet(refs []immutaapi.FrameworkTagRef) (types.Set, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	values := make([]attr.Value, 0, len(refs))
	for _, ref := range refs {
		value, diags := tagRefValue(ref)
		diagnostics.Append(diags...)
		values = append(values, value)
	}

	set, diags := optionalSet(frameworkTagRefType, values)
	diagnostics.Append(diags...)

	return set, diagnostics
}

// optionalSet stores an empty list as null.
func optionalSet(elementType attr.Type, values []attr.Value) (types.Set, diag.Diagnostics) {
	if len(values) == 0 {
		return types.SetNull(elementType), nil
	}

	return types.SetValue(elementType, values)
}

// frameworkErrorDiagnostic turns an error from the frameworks endpoints into
// an actionable diagnostic. API errors never contain credentials or full
// response payloads.
func frameworkErrorDiagnostic(summary string, err error) diag.Diagnostic {
	detail := err.Error()

	var apiErr *immutaapi.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			detail += "\n\nCheck that the configured API key is valid and that the Immuta user holds the GOVERNANCE " +
				"permission required by the framework endpoints. System-created frameworks cannot be edited; " +
				"clone one in Immuta and manage the clone instead."
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			detail += fmt.Sprintf("\n\nImmuta rejected the framework as invalid. Check the configured values, that "+
				"every tag the framework uses exists in the named source, and that there are at most %d rules. "+
				"System-created frameworks cannot be edited.", immutaapi.FrameworkMaxRules)
		case http.StatusConflict:
			detail += "\n\nImmuta reported a conflict. A framework with the same name may already exist."
		}
	}

	return diag.NewErrorDiagnostic(summary, detail)
}
