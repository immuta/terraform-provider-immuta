package provider

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
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
	_ resource.Resource                = (*domainDynamicAssignmentResource)(nil)
	_ resource.ResourceWithConfigure   = (*domainDynamicAssignmentResource)(nil)
	_ resource.ResourceWithImportState = (*domainDynamicAssignmentResource)(nil)
)

// Immuta applies a dynamic assignment in the background, and GET /domain/{id}
// shows the previous assignment until it has. Variables so that tests can
// shorten them.
var (
	domainAssignmentPollInterval = 2 * time.Second
	domainAssignmentTimeout      = 5 * time.Minute
)

// NewDomainDynamicAssignmentResource returns the
// immuta_domain_dynamic_assignment resource.
func NewDomainDynamicAssignmentResource() resource.Resource {
	return &domainDynamicAssignmentResource{}
}

type domainDynamicAssignmentResource struct {
	client *immutaapi.Client
}

type domainDynamicAssignmentResourceModel struct {
	ID       types.String `tfsdk:"id"`
	DomainID types.String `tfsdk:"domain_id"`
	Tags     types.Set    `tfsdk:"tags"`
}

func (r *domainDynamicAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_dynamic_assignment"
}

func (r *domainDynamicAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Assigns data sources to a domain dynamically by tag, with the documented " +
			"`POST /domain/{domainId}/assignment`: every data source with any of the tags is in the domain. " +
			"Destroying it sets the domain back to manual assignment.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ID of the domain, as `domain_id`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"domain_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of the domain, usually `immuta_domain.<name>.id`. Changing it replaces the assignment.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"tags": schema.SetAttribute{
				ElementType: types.StringType,
				Required:    true,
				MarkdownDescription: "Names of the tags whose data sources are in the domain, for example " +
					"`Discovered.Country.Denmark` or `immuta_tag.<name>.name`. At least one is required.",
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
		},
	}
}

func (r *domainDynamicAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *domainDynamicAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainDynamicAssignmentResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.assign(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *domainDynamicAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainDynamicAssignmentResourceModel

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
		tflog.Debug(ctx, "Immuta domain no longer exists, removing its assignment from state", map[string]any{"domain_id": state.DomainID.ValueString()})
		resp.State.RemoveResource(ctx)

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *domainDynamicAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan domainDynamicAssignmentResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.assign(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete sets the domain back to manual assignment.
func (r *domainDynamicAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state domainDynamicAssignmentResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domainID := state.DomainID.ValueString()
	if _, err := r.client.SetDomainAssignment(ctx, domainID, immutaapi.DomainAssignmentManual, nil); err != nil && !immutaapi.IsNotFound(err) {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to set the Immuta domain %s back to manual assignment", domainID), err))
	}
}

// ImportState imports the assignment of a domain by domain ID.
func (r *domainDynamicAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain_id"), req.ID)...)
}

// assign sends the planned tags, then reads back what Immuta stored, waiting
// until Immuta has applied them.
func (r *domainDynamicAssignmentResource) assign(ctx context.Context, model *domainDynamicAssignmentResourceModel) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	var tags []string
	diagnostics.Append(model.Tags.ElementsAs(ctx, &tags, false)...)
	if diagnostics.HasError() {
		return diagnostics
	}
	slices.Sort(tags)

	domainID := model.DomainID.ValueString()
	result, err := r.client.SetDomainAssignment(ctx, domainID, immutaapi.DomainAssignmentDynamic, tags)
	if err != nil {
		diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to assign data sources to the Immuta domain %s by tag", domainID), err))

		return diagnostics
	}

	tflog.Info(ctx, "Assigned data sources to the Immuta domain by tag", map[string]any{
		"domain_id": domainID,
		"added":     result.DataSourcesAdded,
		"kept":      result.DataSourcesKept,
		"removed":   result.DataSourcesRemoved,
	})

	model.ID = types.StringValue(domainID)
	wantDataSources := result.DataSourcesAdded + result.DataSourcesKept
	deadline := time.Now().Add(domainAssignmentTimeout)
	for {
		found, diags := r.refresh(ctx, model)
		diagnostics.Append(diags...)
		if diagnostics.HasError() {
			return diagnostics
		}
		if !found {
			diagnostics.AddError("The Immuta domain disappeared",
				fmt.Sprintf("The domain %s was deleted while its data source assignment was being updated.", domainID))

			return diagnostics
		}

		var got []string
		diagnostics.Append(model.Tags.ElementsAs(ctx, &got, false)...)
		if diagnostics.HasError() {
			return diagnostics
		}
		slices.Sort(got)
		if !slices.Equal(got, tags) {
			if time.Now().After(deadline) {
				diagnostics.AddError("Timed out waiting for Immuta to assign data sources to the domain "+domainID,
					fmt.Sprintf("Immuta accepted the assignment by the tags %q, but after %s the domain still shows the tags %q. "+
						"Immuta may still be applying it; check the domain in Immuta and run terraform apply again.", tags, domainAssignmentTimeout, got))

				return diagnostics
			}

			tflog.Debug(ctx, "Waiting for Immuta to apply the domain assignment", map[string]any{
				"domain_id": domainID,
				"want_tags": tags,
				"got_tags":  got,
			})
		} else {
			// The tags show before Immuta's background job has added the data
			// sources, so wait until the domain lists them too.
			dataSources, err := r.client.ListDomainDataSources(ctx, domainID, "")
			if err != nil {
				if immutaapi.IsNotFound(err) {
					diagnostics.AddError("The Immuta domain disappeared",
						fmt.Sprintf("The domain %s was deleted while its data source assignment was being updated.", domainID))
				} else {
					diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to list the data sources of the Immuta domain %s", domainID), err))
				}

				return diagnostics
			}
			if len(dataSources) == wantDataSources {
				return diagnostics
			}

			if time.Now().After(deadline) {
				diagnostics.AddError("Timed out waiting for Immuta to assign data sources to the domain "+domainID,
					fmt.Sprintf("Immuta accepted the assignment by the tags %q and reported %d data sources in the domain, "+
						"but after %s the domain lists %d. Immuta may still be applying it; check the domain in Immuta and "+
						"run terraform apply again.", tags, wantDataSources, domainAssignmentTimeout, len(dataSources)))

				return diagnostics
			}

			tflog.Debug(ctx, "Waiting for Immuta to add the data sources of the domain assignment", map[string]any{
				"domain_id":         domainID,
				"want_data_sources": wantDataSources,
				"got_data_sources":  len(dataSources),
			})
		}
		if err := sleepContext(ctx, domainAssignmentPollInterval); err != nil {
			diagnostics.AddError("Stopped waiting for Immuta to assign data sources to the domain "+domainID, err.Error())

			return diagnostics
		}
	}
}

// refresh reads the domain's assignment into the model. A domain no longer
// assigned dynamically has no tags, which shows as a change. It reports false
// when the domain no longer exists.
func (r *domainDynamicAssignmentResource) refresh(ctx context.Context, model *domainDynamicAssignmentResourceModel) (bool, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	domainID := model.DomainID.ValueString()
	domain, err := r.client.GetDomain(ctx, domainID)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			return false, diagnostics
		}
		diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta domain %s", domainID), err))

		return true, diagnostics
	}

	tags := []string{}
	if domain.AssignmentType == immutaapi.DomainAssignmentDynamic {
		for _, tag := range domain.Tags {
			tags = append(tags, tag.Name)
		}
	}

	tagSet, diags := types.SetValueFrom(ctx, types.StringType, tags)
	diagnostics.Append(diags...)

	model.ID = types.StringValue(domainID)
	model.Tags = tagSet

	return true, diagnostics
}
