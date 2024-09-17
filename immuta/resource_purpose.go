package immuta

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/immuta/terraform-provider-immuta/client"
	"strconv"
	"time"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &PurposeResource{}
var _ resource.ResourceWithImportState = &PurposeResource{}

func NewPurposeResource() resource.Resource {
	return &PurposeResource{}
}

// PurposeResource defines the resource implementation.
type PurposeResource struct {
	client *client.ImmutaClient
}

// PurposeResourceModel describes the resource data model.
type PurposeResourceModel struct {
	Id              types.Number `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	Acknowledgement types.String `tfsdk:"acknowledgement"`
	Subpurposes     types.List   `tfsdk:"subpurposes"`
}

func (r *PurposeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_purpose"
}

func (r *PurposeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "Immuta PurposeResponse",

		Attributes: map[string]schema.Attribute{
			"id": numberResourceId(),
			"name": schema.StringAttribute{
				MarkdownDescription: "PurposeResponse name, must be unique",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "PurposeResponse description",
				Optional:            true,
			},
			"acknowledgement": schema.StringAttribute{
				MarkdownDescription: "Acknowledgement user must agree to before assuming purpose",
				Optional:            true,
			},
			"subpurposes": schema.ListNestedAttribute{
				MarkdownDescription: "List of subpurposes",
				Optional:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Subpurpose name, must be unique & include parent purpose name",
							Required:            true,
						},
						"description": schema.StringAttribute{
							MarkdownDescription: "Subpurpose description",
							Optional:            true,
						},
						"acknowledgement": schema.StringAttribute{
							MarkdownDescription: "Acknowledgement user must agree to before assuming subpurpose",
							Optional:            true,
						},
					},
				},
			},
		},
	}
}

func (r *PurposeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *PurposeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// todo add check that the subpurpose name is actually a subpurpose of the parent purpose
	var data *PurposeResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Actually create the purpose
	var purposeResponse PurposeResourceResponseV2

	purposeInput := PurposeInput{
		Purpose: Purpose{
			Name:            data.Name.ValueString(),
			Description:     data.Description.ValueString(),
			Acknowledgement: data.Acknowledgement.ValueString(),
		},
	}

	if data.Subpurposes.Elements() != nil && len(data.Subpurposes.Elements()) > 0 {
		subpurposes := make([]Purpose, 0)
		if diags := data.Subpurposes.ElementsAs(ctx, &subpurposes, false); diags != nil && diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}
		purposeInput.Subpurposes = subpurposes
	}

	// Do it twice as a workaround for a bug in the API where acknowledgement not updated first time (ops are idempotent)
	for i := 0; i < 2; i++ {
		pr, err := r.UpsertPurpose(purposeInput)
		if err != nil {
			resp.Diagnostics.AddError(
				"Client error",
				fmt.Sprintf("Could not create purpose: %s", err),
			)
			return
		} else {
			purposeResponse = pr
		}
	}

	data.Id = intToNumberValue(purposeResponse.PurposeId)

	// Write logs using the tflog package
	// Documentation: https://terraform.io/plugin/log
	tflog.Trace(ctx, "created an Immuta purpose resource")

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PurposeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// todo fix the subpurpose name construction
	var data *PurposeResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	purpose, err := r.GetPurpose(data.Id.String())
	if err != nil {
		resp.Diagnostics.AddError(
			"Client error",
			fmt.Sprintf("Could not get purpose: %s", err),
		)
		return
	}
	if strconv.Itoa(purpose.Id) != data.Id.String() {
		resp.Diagnostics.AddError(
			"Provider error",
			fmt.Sprintf("PurposeResponse returned with different ID original [%s] new [%d]", data.Id, purpose.Id),
		)
		return
	}

	if data.Name.ValueString() != purpose.Name {
		data.Name = types.StringValue(purpose.Name)
	}
	if data.Acknowledgement.ValueString() != purpose.Acknowledgement {
		data.Acknowledgement = types.StringValue(purpose.Acknowledgement)
	}
	if data.Description.ValueString() != purpose.Description {
		data.Description = types.StringValue(purpose.Description)
	}

	newSubpurposes, subpurposesDiags := updateListIfChanged[Purpose](ctx, data.Subpurposes, purpose.Subpurposes)
	if subpurposesDiags != nil && subpurposesDiags.HasError() {
		resp.Diagnostics.Append(subpurposesDiags...)
		return
	}
	data.Subpurposes = newSubpurposes

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PurposeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *PurposeResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	purposeInput := PurposeInput{
		Purpose: Purpose{
			Name:            data.Name.ValueString(),
			Description:     data.Description.ValueString(),
			Acknowledgement: data.Acknowledgement.ValueString(),
		},
	}

	if data.Subpurposes.Elements() != nil && len(data.Subpurposes.Elements()) > 0 {
		subpurposes := make([]Purpose, 0)
		if diags := data.Subpurposes.ElementsAs(ctx, &subpurposes, false); diags != nil && diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}
		purposeInput.Subpurposes = subpurposes
	}

	purposeResponse, err := r.UpsertPurpose(purposeInput)
	if err != nil {
		resp.Diagnostics.AddError(
			"Client error",
			fmt.Sprintf("Could not update purpose: %s", err),
		)
		return
	}
	if strconv.Itoa(purposeResponse.PurposeId) != data.Id.String() {
		resp.Diagnostics.AddError(
			"Provider error",
			fmt.Sprintf("PurposeResponse returned with different ID original [%s] new [%d]", data.Id, purposeResponse.PurposeId),
		)
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PurposeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *PurposeResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.DeletePurpose(data.Id.String())
	if err != nil {
		resp.Diagnostics.AddError(
			"Client error",
			fmt.Sprintf("Could not delete purpose: %s", err),
		)
		return
	}
}

func (r *PurposeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// CRUD methods

func (r *PurposeResource) ListPurposes() (purposes Purposes, err error) {
	err = r.client.Get("/governance/purpose", "", map[string]string{"noLimit": "false"}, &purposes)
	return
}

func (r *PurposeResource) GetPurpose(id string) (purpose PurposeResponse, err error) {
	err = r.client.Get(fmt.Sprintf("/governance/purpose/%s", id), "", map[string]string{"includeSubpurposes": "true"}, &purpose)
	return
}

func (r *PurposeResource) DeletePurpose(id string) (err error) {
	err = r.client.Delete(fmt.Sprintf("/governance/purpose/%s", id), "", nil, nil)
	return
}

func (r *PurposeResource) UpsertPurpose(purpose PurposeInput) (purposeResponse PurposeResourceResponseV2, err error) {
	err = r.client.Post("/api/v2/purpose", "", purpose, &purposeResponse)
	return
}

// Domain specific objects

type Purpose struct {
	Name            string `json:"name" tfsdk:"name"`
	Description     string `json:"description" tfsdk:"description"`
	Acknowledgement string `json:"acknowledgement" tfsdk:"acknowledgement"`
}

type PurposeInput struct {
	Purpose
	Subpurposes []Purpose `json:"subpurposes,omitempty"`
}

type PurposeResponse struct {
	PurposeInput
	Id                     int         `json:"id"`
	AddedByProfile         int         `json:"addedByProfile"`
	DisplayAcknowledgement bool        `json:"displayAcknowledgement"`
	Deleted                bool        `json:"deleted"`
	SystemGenerated        bool        `json:"systemGenerated"`
	PolicyMetadata         interface{} `json:"policyMetadata"`
	CreatedAt              time.Time   `json:"createdAt"`
	UpdatedAt              time.Time   `json:"updatedAt"`
}

type PurposeResourceResponseV2 struct {
	DryRun    bool `json:"dryRun"`
	Creating  bool `json:"creating"`
	Updating  bool `json:"updating"`
	PurposeId int  `json:"purposeId"`
}

type Purposes struct {
	Purposes []PurposeResponse `json:"purposes"`
	Count    int               `json:"count"`
}
