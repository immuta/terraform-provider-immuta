package immuta

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/instacart/terraform-provider-immuta/client"
	"strconv"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &ProjectResource{}
var _ resource.ResourceWithImportState = &ProjectResource{}

func NewProjectResource() resource.Resource {
	return &ProjectResource{}
}

// ProjectResource defines the resource implementation.
type ProjectResource struct {
	client *client.ImmutaClient
}

// ProjectResourceModel describes the resource data model.
type ProjectResourceModel struct {
	Id                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	ProjectKey         types.String `tfsdk:"project_key"`
	Documentation      types.String `tfsdk:"documentation"`
	AllowMaskedJoins   types.Bool   `tfsdk:"allow_masked_joins"`
	SubscriptionPolicy types.Map    `tfsdk:"subscription_policy"`
	Tags               types.List   `tfsdk:"tags"`
	Purposes           types.List   `tfsdk:"purposes"`
}

func (r *ProjectResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *ProjectResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "Immuta project.",

		Attributes: map[string]schema.Attribute{
			"id": stringResourceId(),
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the project.",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "The description of the project.",
				Optional:            true,
			},
			"project_key": schema.StringAttribute{
				MarkdownDescription: "The project key of the project, must be unique.",
				Required:            true,
			},
			"documentation": schema.StringAttribute{
				MarkdownDescription: "The markdown documentation of the project.",
				Optional:            true,
			},
			"allow_masked_joins": schema.BoolAttribute{
				MarkdownDescription: "Whether to allow masked joins.",
				Optional:            true,
			},
			"subscription_policy": schema.MapAttribute{
				MarkdownDescription: "The subscription policy of the project.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "The tags of the project.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"purposes": schema.ListAttribute{
				MarkdownDescription: "The purposes of the project.",
				Optional:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

func (r *ProjectResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	immutaClient, ok := req.ProviderData.(*client.ImmutaClient)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *immutaClient.ImmutaClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = immutaClient
}

func (r *ProjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *ProjectResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	subscriptionPolicy := make(map[string]interface{})
	if diags := data.SubscriptionPolicy.ElementsAs(ctx, &subscriptionPolicy, false); diags != nil {
		resp.Diagnostics.Append(diags...)
		return
	}

	tags := make([]string, 0)
	if diags := data.Tags.ElementsAs(ctx, &tags, false); diags != nil {
		resp.Diagnostics.Append(diags...)
		return
	}

	purposes := make([]string, 0)
	if diags := data.Purposes.ElementsAs(ctx, &purposes, false); diags != nil {
		resp.Diagnostics.Append(diags...)
		return
	}

	project := ProjectInput{
		Name:               data.Name.ValueString(),
		ProjectKey:         data.ProjectKey.ValueString(),
		Description:        data.Description.ValueString(),
		Documentation:      data.Documentation.ValueString(),
		AllowMaskedJoins:   data.AllowMaskedJoins.ValueBool(),
		SubscriptionPolicy: subscriptionPolicy,
		Tags:               tags,
		Purposes:           purposes,
	}

	projectResponse, err := r.UpsertProject(project)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating project",
			fmt.Sprintf("Error creating project: %s", err),
		)
		return
	}

	data.Id = types.StringValue(strconv.Itoa(projectResponse.ProjectId))

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ProjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *ProjectResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	project, err := r.GetProject(data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading project",
			fmt.Sprintf("Error reading project: %s", err),
		)
		return
	}

	// todo check if id or project key have changed

	if data.Name.ValueString() != project.Name {
		data.Name = types.StringValue(project.Name)
	}
	if data.Description.ValueString() != project.Description {
		data.Description = types.StringValue(project.Description)
	}
	if data.Documentation.ValueString() != project.Documentation {
		data.Documentation = types.StringValue(project.Documentation)
	}
	if data.AllowMaskedJoins.ValueBool() != project.AllowMaskedJoins {
		data.AllowMaskedJoins = types.BoolValue(project.AllowMaskedJoins)
	}

	newSubscriptionPolicy, subscriptionDiag := updateMapIfChanged(ctx, data.SubscriptionPolicy, project.SubscriptionPolicy)
	if subscriptionDiag != nil {
		resp.Diagnostics.Append(subscriptionDiag...)
		return
	}
	data.SubscriptionPolicy = newSubscriptionPolicy

	newTags, tagsDiag := updateListIfChanged(ctx, data.Tags, project.Tags)
	if tagsDiag != nil {
		resp.Diagnostics.Append(tagsDiag...)
		return
	}
	data.Tags = newTags

	newPurposes, purposesDiag := updateListIfChanged(ctx, data.Purposes, project.Purposes)
	if purposesDiag != nil {
		resp.Diagnostics.Append(purposesDiag...)
		return
	}
	data.Purposes = newPurposes

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ProjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *ProjectResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// todo
	// Actually update the Project

	subscriptionPolicy := make(map[string]interface{})
	if diags := data.SubscriptionPolicy.ElementsAs(ctx, &subscriptionPolicy, false); diags != nil {
		resp.Diagnostics.Append(diags...)
		return
	}

	tags := make([]string, 0)
	if diags := data.Tags.ElementsAs(ctx, &tags, false); diags != nil {
		resp.Diagnostics.Append(diags...)
		return
	}

	purposes := make([]string, 0)
	if diags := data.Purposes.ElementsAs(ctx, &purposes, false); diags != nil {
		resp.Diagnostics.Append(diags...)
		return
	}

	project := ProjectInput{
		Name:               data.Name.ValueString(),
		ProjectKey:         data.ProjectKey.ValueString(),
		Description:        data.Description.ValueString(),
		Documentation:      data.Documentation.ValueString(),
		AllowMaskedJoins:   data.AllowMaskedJoins.ValueBool(),
		SubscriptionPolicy: subscriptionPolicy,
		Tags:               tags,
		Purposes:           purposes,
	}

	projectResponse, err := r.UpsertProject(project)

	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating project",
			fmt.Sprintf("Error creating project: %s", err),
		)
		return
	}

	if strconv.Itoa(projectResponse.ProjectId) != data.Id.ValueString() {
		resp.Diagnostics.AddError(
			"Error updating project",
			fmt.Sprintf("Error updating project, new ID value returned - old:[%s] new:[%d]: %s", data.Id.ValueString(), projectResponse.ProjectId, err),
		)
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ProjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *ProjectResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.DeleteProject(data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error deleting project",
			fmt.Sprintf("Error deleting project: %s", err),
		)
		return
	}
}

func (r *ProjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *ProjectResource) ListProjects() (projects Projects, err error) {
	err = r.client.Get("/project", "", map[string]string{"noLimit": "false"}, &projects)
	return
}

func (r *ProjectResource) GetProject(id string) (project Project, err error) {
	err = r.client.Get(fmt.Sprintf("/project/%s", id), "", nil, &project)
	return
}

func (r *ProjectResource) DeleteProject(id string) (err error) {
	err = r.client.Delete(fmt.Sprintf("/project/%s", id), "", nil, nil)
	return
}

func (r *ProjectResource) UpsertProject(project ProjectInput) (projectResponse ProjectResourceResponseV2, err error) {
	err = r.client.Post("/api/v2/project", "", project, &projectResponse)
	return
}
