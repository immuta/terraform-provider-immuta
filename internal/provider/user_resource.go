package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ resource.Resource                = (*userResource)(nil)
	_ resource.ResourceWithConfigure   = (*userResource)(nil)
	_ resource.ResourceWithImportState = (*userResource)(nil)
)

// NewUserResource returns the immuta_user resource.
func NewUserResource() resource.Resource {
	return &userResource{}
}

type userResource struct {
	client *immutaapi.Client
}

// userResourceModel is the Terraform representation of a BIM user.
type userResourceModel struct {
	ID           types.String `tfsdk:"id"`
	IAMID        types.String `tfsdk:"iam_id"`
	UserID       types.String `tfsdk:"user_id"`
	Name         types.String `tfsdk:"name"`
	Email        types.String `tfsdk:"email"`
	Phone        types.String `tfsdk:"phone"`
	SQLUser      types.String `tfsdk:"sql_user"`
	About        types.String `tfsdk:"about"`
	Location     types.String `tfsdk:"location"`
	Organization types.String `tfsdk:"organization"`
	Position     types.String `tfsdk:"position"`
	Permissions  types.Set    `tfsdk:"permissions"`
	Disabled     types.Bool   `tfsdk:"disabled"`
	CreatedAt    types.String `tfsdk:"created_at"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

func (r *userResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *userResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a user in Immuta's built-in identity manager (BIM) with the documented `/bim` API.\n\n" +
			"Users held in an external identity manager are synchronised by Immuta and must not be managed with this resource.\n\n" +
			"~> **Destroying this resource permanently deletes the user.** Immuta does not publish a delete-user endpoint, so the provider " +
			"issues the same undocumented request the Immuta UI uses for *Permanently delete user*. Deletion cannot be undone. " +
			"To retain the account instead, set `disabled = true` and remove the resource from configuration with `terraform state rm`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Numeric profile ID of the user, assigned by Immuta. This is the identifier the documented read endpoint takes.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"iam_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: fmt.Sprintf("Identity manager the user belongs to. Always `%s`: the documented create endpoint only writes to Immuta's built-in identity manager.", immutaapi.IAMIDBIM),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"user_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Username of the user, usually an email address. Changing it replaces the user.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Display name of the user. The documented profile update endpoint does not accept a name, so changing it replaces the user.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"email": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Email address on the user's profile. Immuta derives one from the username when it is not set, so removing it from configuration leaves the last value in place rather than clearing it.",
			},
			"phone": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Phone number on the user's profile.",
			},
			"sql_user": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The user's SQL username.",
			},
			"about": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Details about the user, displayed on their profile.",
			},
			"location": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The user's location.",
			},
			"organization": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The user's organization.",
			},
			"position": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The user's position.",
			},
			"permissions": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Immuta permissions held by the user, for example `CREATE_PROJECT`. When set, this is the complete " +
					"set: permissions Immuta grants by default to new users, or that are granted outside Terraform, are removed. " +
					"Set `permissions = []` for a user with no permissions. When unset, Terraform does not manage permissions and " +
					"records whatever Immuta assigns, including any tenant default permissions.",
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"disabled": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Whether the user is disabled. A disabled user keeps their account but cannot log in.",
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timestamp of when the user was created.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timestamp of when the user was last updated.",
			},
		},
	}
}

func (r *userResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *userResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	permissions, diags := stringSlice(ctx, plan.Permissions)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createRequest := immutaapi.CreateUserRequest{
		IAMID:       immutaapi.IAMIDBIM,
		UserID:      plan.UserID.ValueString(),
		Permissions: permissions,
	}

	// Only name and email are documented on the create payload. Everything
	// else on the profile is set with the documented profile update endpoint
	// straight afterwards.
	if isSet(plan.Name) || isSet(plan.Email) {
		createRequest.Profile = &immutaapi.CreateUserProfile{
			Name:  plan.Name.ValueString(),
			Email: plan.Email.ValueString(),
		}
	}

	created, err := r.client.CreateUser(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.Append(userErrorDiagnostic("Unable to create the Immuta user", err))

		return
	}

	tflog.Debug(ctx, "Created an Immuta user", map[string]any{"id": created.ID})

	iamID := firstNonEmpty(created.IAMID, immutaapi.IAMIDBIM)
	userID := firstNonEmpty(created.UserID, plan.UserID.ValueString())

	profileRequest := immutaapi.UpdateUserProfileRequest{
		Phone:        optionalStringPointer(plan.Phone),
		SQLUser:      optionalStringPointer(plan.SQLUser),
		About:        optionalStringPointer(plan.About),
		Location:     optionalStringPointer(plan.Location),
		Organization: optionalStringPointer(plan.Organization),
		Position:     optionalStringPointer(plan.Position),
	}
	if !profileRequest.IsEmpty() {
		if err := r.client.UpdateUserProfile(ctx, iamID, userID, profileRequest); err != nil {
			resp.Diagnostics.Append(userErrorDiagnostic(
				fmt.Sprintf("Unable to set the profile of the Immuta user %s after creating it", userID),
				err,
			))

			return
		}
	}

	// The documented create endpoint always creates an enabled user.
	if plan.Disabled.ValueBool() {
		if err := r.client.SetUserDisabled(ctx, iamID, userID, true); err != nil {
			resp.Diagnostics.Append(userErrorDiagnostic(
				fmt.Sprintf("Unable to disable the Immuta user %s after creating it", userID),
				err,
			))

			return
		}
	}

	// Immuta can grant tenant default permissions to every new user on top of
	// the ones in the create request. When permissions are configured they
	// are the complete set, so they are written again with the documented
	// replace endpoint. Without this the read back would not match the plan.
	if permissionsConfigured(plan.Permissions) {
		if err := r.client.UpdateUserPermissions(ctx, iamID, userID, permissions); err != nil {
			resp.Diagnostics.Append(userErrorDiagnostic(
				fmt.Sprintf("Unable to set the permissions of the Immuta user %s after creating it", userID),
				err,
			))

			return
		}
	}

	r.refresh(ctx, &plan, created.ID, iamID, "creating", &resp.Diagnostics, &resp.State)
}

func (r *userResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, ok := parseInt64Attribute(state.ID, path.Root("id"), "user", &resp.Diagnostics)
	if !ok {
		return
	}

	iamID := firstNonEmpty(state.IAMID.ValueString(), immutaapi.IAMIDBIM)

	user, err := r.client.GetUser(ctx, iamID, id)
	if err != nil {
		if immutaapi.IsNotFound(err) {
			tflog.Debug(ctx, "Immuta user no longer exists, removing it from state", map[string]any{"id": id})
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.Append(userErrorDiagnostic(fmt.Sprintf("Unable to read the Immuta user %d", id), err))

		return
	}

	resp.Diagnostics.Append(state.applyUser(ctx, user)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *userResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state userResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, ok := parseInt64Attribute(state.ID, path.Root("id"), "user", &resp.Diagnostics)
	if !ok {
		return
	}

	iamID := firstNonEmpty(state.IAMID.ValueString(), immutaapi.IAMIDBIM)
	userID := state.UserID.ValueString()

	// Only send what actually changed, on the endpoint documented for it.
	var profileRequest immutaapi.UpdateUserProfileRequest
	for _, field := range []struct {
		plan, state types.String
		target      **string
	}{
		{plan.Email, state.Email, &profileRequest.Email},
		{plan.Phone, state.Phone, &profileRequest.Phone},
		{plan.SQLUser, state.SQLUser, &profileRequest.SQLUser},
		{plan.About, state.About, &profileRequest.About},
		{plan.Location, state.Location, &profileRequest.Location},
		{plan.Organization, state.Organization, &profileRequest.Organization},
		{plan.Position, state.Position, &profileRequest.Position},
	} {
		if !field.plan.Equal(field.state) && !field.plan.IsUnknown() {
			value := field.plan.ValueString()
			*field.target = &value
		}
	}

	if !profileRequest.IsEmpty() {
		if err := r.client.UpdateUserProfile(ctx, iamID, userID, profileRequest); err != nil {
			resp.Diagnostics.Append(userErrorDiagnostic(fmt.Sprintf("Unable to update the profile of the Immuta user %s", userID), err))

			return
		}
	}

	// Unset permissions are planned from state, so they never differ here and
	// are left alone.
	if permissionsConfigured(plan.Permissions) && !plan.Permissions.Equal(state.Permissions) {
		permissions, diags := stringSlice(ctx, plan.Permissions)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		if err := r.client.UpdateUserPermissions(ctx, iamID, userID, permissions); err != nil {
			resp.Diagnostics.Append(userErrorDiagnostic(fmt.Sprintf("Unable to update the permissions of the Immuta user %s", userID), err))

			return
		}
	}

	if !plan.Disabled.Equal(state.Disabled) {
		if err := r.client.SetUserDisabled(ctx, iamID, userID, plan.Disabled.ValueBool()); err != nil {
			resp.Diagnostics.Append(userErrorDiagnostic(fmt.Sprintf("Unable to change whether the Immuta user %s is disabled", userID), err))

			return
		}
	}

	plan.ID = state.ID
	r.refresh(ctx, &plan, id, iamID, "updating", &resp.Diagnostics, &resp.State)
}

func (r *userResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	iamID := firstNonEmpty(state.IAMID.ValueString(), immutaapi.IAMIDBIM)
	userID := state.UserID.ValueString()

	if err := r.client.DeleteUser(ctx, iamID, userID); err != nil {
		if immutaapi.IsNotFound(err) {
			// Already gone remotely, nothing left to do.
			return
		}

		resp.Diagnostics.Append(userErrorDiagnostic(fmt.Sprintf("Unable to delete the Immuta user %s", userID), err))
	}
}

// ImportState accepts the numeric user id, optionally prefixed with the IAM
// id, for example "18" or "bim/18".
func (r *userResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	iamID := immutaapi.IAMIDBIM
	rawID := strings.TrimSpace(req.ID)

	if parts := strings.SplitN(rawID, "/", 2); len(parts) == 2 {
		iamID = parts[0]
		rawID = parts[1]
	}

	if _, err := strconv.ParseInt(rawID, 10, 64); err != nil {
		resp.Diagnostics.AddError(
			"Invalid import ID for an Immuta user",
			fmt.Sprintf("Import an Immuta user with its numeric profile ID, optionally prefixed with the IAM id, for example \"18\" or \"bim/18\". Got %q.", req.ID),
		)

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), rawID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("iam_id"), iamID)...)
}

// refresh reads the user back after a write so that state matches what the
// API stored.
func (r *userResource) refresh(
	ctx context.Context,
	model *userResourceModel,
	id int64,
	iamID string,
	action string,
	diagnostics *diag.Diagnostics,
	state *tfsdk.State,
) {
	user, err := r.client.GetUser(ctx, iamID, id)
	if err != nil {
		diagnostics.Append(userErrorDiagnostic(
			fmt.Sprintf("Unable to read the Immuta user %d back after %s it", id, action),
			err,
		))

		return
	}

	diagnostics.Append(model.applyUser(ctx, user)...)
	if diagnostics.HasError() {
		return
	}

	diagnostics.Append(state.Set(ctx, model)...)
}

// applyUser copies an API user onto the Terraform model.
func (m *userResourceModel) applyUser(ctx context.Context, user *immutaapi.User) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	m.ID = types.StringValue(strconv.FormatInt(user.ID, 10))
	m.IAMID = types.StringValue(firstNonEmpty(user.IAMID, m.IAMID.ValueString(), immutaapi.IAMIDBIM))
	m.UserID = knownStringOr(user.UserID, m.UserID)
	m.Disabled = types.BoolValue(user.Disabled)
	m.CreatedAt = optionalComputedString(user.CreatedAt)
	m.UpdatedAt = optionalComputedString(user.UpdatedAt)

	profile := user.Profile
	if profile == nil {
		profile = &immutaapi.UserProfile{}
	}

	m.Name = normalizeOptionalString(profile.Name, m.Name)
	m.Email = normalizeOptionalString(profile.Email, m.Email)
	m.Phone = normalizeOptionalString(profile.Phone, m.Phone)
	m.SQLUser = normalizeOptionalString(profile.SQLUser, m.SQLUser)
	m.About = normalizeOptionalString(profile.About, m.About)
	m.Location = normalizeOptionalString(profile.Location, m.Location)
	m.Organization = normalizeOptionalString(profile.Organization, m.Organization)
	m.Position = normalizeOptionalString(profile.Position, m.Position)

	permissions, diags := stringSetValue(ctx, user.Permissions)
	diagnostics.Append(diags...)
	if diagnostics.HasError() {
		return diagnostics
	}
	m.Permissions = permissions

	return diagnostics
}

// permissionsConfigured reports whether the permissions are managed. An
// unset attribute is unknown at plan time on create and carried over from
// state afterwards.
func permissionsConfigured(permissions types.Set) bool {
	return !permissions.IsNull() && !permissions.IsUnknown()
}

// stringSetValue converts an API string slice into a Terraform set, mapping
// an omitted list to an empty set so that it matches the schema default.
func stringSetValue(ctx context.Context, values []string) (types.Set, diag.Diagnostics) {
	if values == nil {
		values = []string{}
	}

	return types.SetValueFrom(ctx, types.StringType, values)
}

// stringSlice converts a Terraform set into an API string slice.
func stringSlice(ctx context.Context, set types.Set) ([]string, diag.Diagnostics) {
	values := []string{}
	if set.IsNull() || set.IsUnknown() {
		return values, nil
	}

	diagnostics := set.ElementsAs(ctx, &values, false)

	return values, diagnostics
}

// optionalStringPointer returns a pointer to a configured optional string, or
// nil when it is not set.
func optionalStringPointer(value types.String) *string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}

	text := value.ValueString()

	return &text
}
