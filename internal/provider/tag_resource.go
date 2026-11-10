package provider

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                = (*tagResource)(nil)
	_ resource.ResourceWithConfigure   = (*tagResource)(nil)
	_ resource.ResourceWithImportState = (*tagResource)(nil)
)

// NewTagResource returns the immuta_tag resource.
func NewTagResource() resource.Resource {
	return &tagResource{}
}

type tagResource struct {
	client *immutaapi.Client
}

// tagResourceModel is the Terraform representation of a tag.
type tagResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Source          types.String `tfsdk:"source"`
	SystemCreated   types.Bool   `tfsdk:"system_created"`
	DeleteHierarchy types.Bool   `tfsdk:"delete_hierarchy"`
}

func (r *tagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tag"
}

func (r *tagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an Immuta tag with the documented `/tag` API.\n\n" +
			"Tags are what identifiers apply to the columns they match, and what policies match on. An identifier can only " +
			"reference a tag that already exists, so this resource is normally paired with `immuta_identifier`:\n\n" +
			"```terraform\n" +
			"resource \"immuta_tag\" \"primary_color\" {\n" +
			"  name = \"Discovered.Primary color\"\n" +
			"}\n\n" +
			"resource \"immuta_identifier\" \"primary_color\" {\n" +
			"  tags = [immuta_tag.primary_color.name]\n" +
			"  # ...\n" +
			"}\n" +
			"```\n\n" +
			"Referencing `immuta_tag.<name>.name` rather than typing the string makes Terraform create the tag first.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Numeric ID of the tag, assigned by Immuta.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Fully qualified name of the tag, with a dot separating a parent tag from its child, " +
					"for example `Discovered.Primary color`. Immuta creates any missing parent tags, and children are added " +
					"under a root that already exists, such as `Discovered`, without touching its other children. Changing " +
					"the name replaces the tag.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"delete_hierarchy": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Whether destroying this resource also deletes every child tag beneath it. " +
					"Defaults to `false`, which makes Immuta reject the delete while children exist. This only affects " +
					"destroy, so changing it sends no request.",
			},
			"source": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Where the tag came from. Tags created through the API are `curated`.",
			},
			"system_created": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether Immuta created the tag itself rather than a user creating it.",
			},
		},
	}
}

func (r *tagResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *tagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan tagResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()

	if err := r.createTag(ctx, name); err != nil {
		resp.Diagnostics.Append(tagErrorDiagnostic(fmt.Sprintf("Unable to create the Immuta tag %s", name), err))

		return
	}

	tflog.Debug(ctx, "Created an Immuta tag", map[string]any{"name": name})

	// The create response describes every tag in the hierarchy, so the tag is
	// read back by name to get its own record.
	tag, err := r.client.FindTag(ctx, name)
	if err != nil {
		resp.Diagnostics.Append(tagErrorDiagnostic(
			fmt.Sprintf("Unable to read the Immuta tag %s back after creating it", name),
			err,
		))

		return
	}

	plan.applyTag(tag)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *tagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state tagResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()

	tag, err := r.client.FindTag(ctx, name)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			tflog.Debug(ctx, "Immuta tag no longer exists, removing it from state", map[string]any{"name": name})
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.Append(tagErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta tag %s", name), err))

		return
	}

	state.applyTag(tag)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update only ever handles delete_hierarchy, which changes what destroy does
// rather than anything in Immuta. The name requires replacement.
func (r *tagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state tagResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = state.ID
	plan.Source = state.Source
	plan.SystemCreated = state.SystemCreated

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *tagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state tagResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()

	if err := r.client.DeleteTag(ctx, name, state.DeleteHierarchy.ValueBool()); err != nil {
		if immutaapi.IsNotFound(err) {
			// Already gone remotely, nothing left to do.
			return
		}

		resp.Diagnostics.Append(tagDeleteErrorDiagnostic(fmt.Sprintf("Unable to delete the Immuta tag %s", name), err))
	}
}

// tagCreateMu serialises tag creation within the provider. Whether a create
// must name an existing root depends on whether that root exists, so two tags
// under the same new root created in parallel would otherwise both see it
// missing, and the second create would be rejected.
var tagCreateMu sync.Mutex

// createTag creates one tag, adding it under its root with the documented
// rootTag field when that root already exists. Discovered, which Immuta
// creates for sensitive data discovery, is the common case.
func (r *tagResource) createTag(ctx context.Context, name string) error {
	tagCreateMu.Lock()
	defer tagCreateMu.Unlock()

	rootTag := ""
	if root := immutaapi.RootTagName(name); root != "" {
		_, err := r.client.FindTag(ctx, root)

		switch {
		case err == nil:
			rootTag = root
		case !immutaapi.IsNotFound(err):
			return fmt.Errorf("checking whether the root tag %s exists: %w", root, err)
		}
	}

	tflog.Debug(ctx, "Creating an Immuta tag", map[string]any{"name": name, "existing_root": rootTag})

	_, err := r.client.CreateTags(ctx, []string{name}, rootTag)

	return err
}

// ImportState accepts the fully qualified tag name.
func (r *tagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

// applyTag copies an API tag onto the Terraform model.
func (m *tagResourceModel) applyTag(tag *immutaapi.Tag) {
	m.ID = types.StringValue(strconv.FormatInt(tag.ID, 10))
	m.Name = types.StringValue(tag.Name)
	m.Source = optionalComputedString(tag.Source)
	m.SystemCreated = types.BoolValue(tag.SystemCreated)

	// delete_hierarchy has no counterpart in the API, so it keeps whatever the
	// configuration asked for. On import there is nothing to keep.
	if m.DeleteHierarchy.IsNull() || m.DeleteHierarchy.IsUnknown() {
		m.DeleteHierarchy = types.BoolValue(false)
	}
}
