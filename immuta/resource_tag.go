package immuta

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/instacart/terraform-provider-immuta/client"
	"strconv"
	"time"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &TagResource{}
var _ resource.ResourceWithImportState = &TagResource{}

func NewTagResource() resource.Resource {
	return &TagResource{}
}

// TagResource defines the resource implementation.
type TagResource struct {
	client *client.ImmutaClient
}

// TagResourceModel describes the resource data model.
type TagResourceModel struct {
	Id      types.String `tfsdk:"id"`
	Name    types.String `tfsdk:"name"`
	RootTag types.String `tfsdk:"root_tag"`
}

func (r *TagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tag"
}

func (r *TagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "A data tag",

		Attributes: map[string]schema.Attribute{
			"id": stringResourceId(),
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the tag, fully qualified, e.g. if RootTag = 'foo', Name = 'bar', then the tag is 'foo.bar'",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"root_tag": schema.StringAttribute{
				MarkdownDescription: "The root tag of the tag",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *TagResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *TagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *TagResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	tagInput := TagInput{
		Tags: []TagSingular{
			{Name: data.Name.ValueString()},
		},
	}

	if data.RootTag.ValueString() != "" {

		tagInput.RootTag = &RootTag{
			Name:            data.RootTag.ValueString(),
			DeleteHierarchy: false,
		}
	}

	tagResponse, err := r.CreateTag(ctx, tagInput)
	if err != nil {
		resp.Diagnostics.AddError("Error creating tag", err.Error())
		return
	}

	data.Id = types.StringValue(strconv.Itoa(tagResponse.Id))

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *TagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *TagResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	tag, err := r.GetTag(ctx, data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error getting tag", err.Error())
		return
	}

	if tag == nil {
		// Tag no longer exists, remove from state
		resp.Diagnostics.Append(resp.State.Set(ctx, nil)...)
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *TagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *TagResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// todo as update not currently supported

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *TagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *TagResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.DeleteTag(ctx, data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error deleting tag", err.Error())
		return
	}
}

func (r *TagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// CRUD methods

func (r *TagResource) CreateTag(_ context.Context, tagInput TagInput) (response *TagCreateResponse, err error) {
	responses := make([]TagCreateResponse, 0)
	err = r.client.Post("/tag", "", tagInput, &responses)
	if responses == nil || len(responses) == 0 {
		return nil, fmt.Errorf("no response from create tag")
	}
	response = &responses[0]
	return
}

// GetTag returns nil if tag does not exist
func (r *TagResource) GetTag(_ context.Context, name string) (*TagList, error) {
	// have to search for the tag because the API doesn't support getting a tag by name/id
	tags := make([]TagList, 0)
	err := r.client.Get("/tag", "", map[string]string{"searchText": name}, &tags)
	if err != nil {
		return nil, err
	}

	if len(tags) == 0 {
		return nil, nil
	}

	for _, tag := range tags {
		if tag.Name == name {
			return &tag, nil
		}
	}

	return nil, nil
}

func (r *TagResource) DeleteTag(_ context.Context, name string) (err error) {
	err = r.client.Delete(fmt.Sprintf("/tag/%s", name), "", nil, nil)
	return
}

// Domain specific types

type TagInput struct {
	Tags     []TagSingular `json:"tags"`
	*RootTag `json:"rootTag,omitempty"`
}

//// MarshalJSON omit rootTag if empty
//func (t TagInput) MarshalJSON() ([]byte, error) {
//	if t.RootTag.Name == "" {
//		return json.Marshal(struct {
//			Tags []TagSingular `json:"tags"`
//		}{
//			Tags: t.Tags,
//		})
//	}
//	return json.Marshal(t)
//}

type TagSingular struct {
	Name string `json:"name"`
}

type RootTag struct {
	Name            string `json:"name,omitempty"`
	DeleteHierarchy bool   `json:"deleteHierarchy,omitempty"`
}

type TagCreateResponse struct {
	Id            int       `json:"id"`
	Name          string    `json:"name"`
	Source        string    `json:"source"`
	Deleted       bool      `json:"deleted"`
	SystemCreated bool      `json:"systemCreated"`
	CreatedBy     int       `json:"createdBy"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type TagList struct {
	Name          string `json:"name"`
	HasLeafNodes  bool   `json:"hasLeafNodes"`
	Source        string `json:"source"`
	Id            int    `json:"id"`
	Deleted       bool   `json:"deleted"`
	SystemCreated bool   `json:"systemCreated"`
	DisplayName   string `json:"displayName"`
}
