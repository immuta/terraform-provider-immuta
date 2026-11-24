package provider

import (
	"context"
	"fmt"
	"regexp"
	"slices"
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
	_ resource.Resource                = (*domainDataSourceAssignmentResource)(nil)
	_ resource.ResourceWithConfigure   = (*domainDataSourceAssignmentResource)(nil)
	_ resource.ResourceWithImportState = (*domainDataSourceAssignmentResource)(nil)
)

var dataSourceIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

// How long the provider waits for Immuta's background job to add a data
// source to a domain. Variables so that tests can shorten them.
var (
	domainDataSourcePollInterval = 2 * time.Second
	domainDataSourceAddTimeout   = 5 * time.Minute
)

// NewDomainDataSourceAssignmentResource returns the
// immuta_domain_data_source_assignment resource.
func NewDomainDataSourceAssignmentResource() resource.Resource {
	return &domainDataSourceAssignmentResource{}
}

type domainDataSourceAssignmentResource struct {
	client *immutaapi.Client
}

type domainDataSourceAssignmentResourceModel struct {
	ID           types.String `tfsdk:"id"`
	DomainID     types.String `tfsdk:"domain_id"`
	DataSourceID types.String `tfsdk:"data_source_id"`
}

func (r *domainDataSourceAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_data_source_assignment"
}

func (r *domainDataSourceAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Adds one data source, such as a table, to a domain with manual data source assignment, with the " +
			"documented `POST /domain/{domainId}/datasources`. Destroying it removes the data source from the domain.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`<domain_id>/<data_source_id>`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"domain_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of the domain, usually `immuta_domain.<name>.id`. Changing it moves the data source.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"data_source_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "ID of the data source, usually `data.immuta_data_source.<name>.id`. Changing it " +
					"assigns another data source.",
				Validators:    []validator.String{stringvalidator.RegexMatches(dataSourceIDPattern, "must be a data source ID, a positive integer")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *domainDataSourceAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *domainDataSourceAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainDataSourceAssignmentResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domainID, dataSourceID := plan.DomainID.ValueString(), plan.DataSourceID.ValueString()
	subject := fmt.Sprintf("the data source %s to the Immuta domain %s", dataSourceID, domainID)

	// Immuta refuses data sources that are already assigned; one already in
	// this domain is reported with how to adopt it instead.
	assigned, diags := r.assigned(ctx, domainID, dataSourceID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if assigned {
		resp.Diagnostics.AddError("The data source is already in the domain",
			fmt.Sprintf("The data source %s is already in the Immuta domain %s. To manage it with Terraform, import it: "+
				"terraform import <address> %s/%s", dataSourceID, domainID, domainID, dataSourceID))

		return
	}

	jobID, err := r.client.AddDomainDataSources(ctx, domainID, []string{dataSourceID})
	if err != nil {
		diagnostic := apiErrorDiagnostic("Unable to add "+subject, err)
		resp.Diagnostics.AddError(diagnostic.Summary(), diagnostic.Detail()+"\n\nImmuta only adds data sources to domains with "+
			"manual assignment (not one with an immuta_domain_dynamic_assignment), and refuses data sources that are already "+
			"in another domain.")

		return
	}

	tflog.Debug(ctx, "Waiting for Immuta to add the data source to the domain", map[string]any{"domain_id": domainID, "data_source_id": dataSourceID, "job_id": jobID})

	deadline := time.Now().Add(domainDataSourceAddTimeout)
	for {
		assigned, diags := r.assigned(ctx, domainID, dataSourceID)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		if assigned {
			break
		}
		if time.Now().After(deadline) {
			resp.Diagnostics.AddError("Timed out adding "+subject,
				fmt.Sprintf("Immuta accepted the request (background job %s), but the data source was not in the domain after %s.",
					jobID, domainDataSourceAddTimeout))

			return
		}
		if err := sleepContext(ctx, domainDataSourcePollInterval); err != nil {
			resp.Diagnostics.AddError("Stopped waiting while adding "+subject, err.Error())

			return
		}
	}

	plan.ID = types.StringValue(domainID + "/" + dataSourceID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *domainDataSourceAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainDataSourceAssignmentResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domainID, dataSourceID := state.DomainID.ValueString(), state.DataSourceID.ValueString()
	assigned, diags := r.assigned(ctx, domainID, dataSourceID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !assigned {
		tflog.Debug(ctx, "The data source is no longer in the Immuta domain, removing it from state", map[string]any{"domain_id": domainID, "data_source_id": dataSourceID})
		resp.State.RemoveResource(ctx)

		return
	}

	state.ID = types.StringValue(domainID + "/" + dataSourceID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called: every attribute requires replacement.
func (r *domainDataSourceAssignmentResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unexpected update", "A domain data source assignment cannot be updated in place. This is a bug in the provider.")
}

func (r *domainDataSourceAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state domainDataSourceAssignmentResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domainID, dataSourceID := state.DomainID.ValueString(), state.DataSourceID.ValueString()
	if err := r.client.RemoveDomainDataSource(ctx, domainID, dataSourceID); err != nil && !immutaapi.IsNotFound(err) {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to remove the data source %s from the Immuta domain %s", dataSourceID, domainID), err))
	}
}

// ImportState imports an assignment by <domain_id>/<data_source_id>.
func (r *domainDataSourceAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	domainID, dataSourceID, ok := strings.Cut(req.ID, "/")
	if !ok || domainID == "" || !dataSourceIDPattern.MatchString(dataSourceID) {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Expected <domain_id>/<data_source_id>, for example c7458ee9-03e5-4b6f-8395-ad4e49235428/36. Got %q.", req.ID))

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain_id"), domainID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("data_source_id"), dataSourceID)...)
}

// assigned reports whether the data source is in the domain, with one
// documented GET /dataSource filtered by both.
func (r *domainDataSourceAssignmentResource) assigned(ctx context.Context, domainID, dataSourceID string) (bool, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	dataSources, err := r.client.SearchDataSources(ctx, immutaapi.SearchDataSourcesOptions{
		DataSourceIDs: []string{dataSourceID},
		DomainID:      domainID,
	})
	if err != nil {
		diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to check whether the data source %s is in the Immuta domain %s", dataSourceID, domainID), err))

		return false, diagnostics
	}

	return slices.ContainsFunc(dataSources, func(dataSource immutaapi.DataSource) bool {
		return dataSource.ID == dataSourceID
	}), diagnostics
}
