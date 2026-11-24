package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ datasource.DataSource              = (*globalUserDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*globalUserDataSource)(nil)
)

// NewGlobalUserDataSource returns the immuta_global_user data source.
func NewGlobalUserDataSource() datasource.DataSource {
	return &globalUserDataSource{}
}

type globalUserDataSource struct {
	client *immutaapi.Client
}

type globalUserDataSourceModel struct {
	ID           types.String `tfsdk:"id"`
	GlobalUserID types.String `tfsdk:"global_user_id"`
	Username     types.String `tfsdk:"username"`
	IAMID        types.String `tfsdk:"iam_id"`
	Name         types.String `tfsdk:"name"`
	Email        types.String `tfsdk:"email"`
}

func (d *globalUserDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_global_user"
}

func (d *globalUserDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up the Immuta global user ID of an existing user by username, with the documented Request app " +
			"(formerly Marketplace) `GET /api/metadata/users` search. The global user ID is a UUID identifying a user " +
			"across identity managers, for example `immuta_data_product.subject_matter_expert_id`.\n\n" +
			"Needs the provider's `request_app_url` and `request_app_token`. Exactly one user must match: the match is " +
			"exact, and an error explains what to do when no user or several users match.",
		Attributes: map[string]schema.Attribute{
			"username": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Username (login name) of the user to look up, the same value as `immuta_user.user_id`. The match is exact.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"iam_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Identity manager to look in, for example `bim`. Needed when the same username exists in several identity managers.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The global user ID, the same value as `global_user_id`.",
			},
			"global_user_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Immuta global user ID of the user, a UUID.",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Display name of the user.",
			},
			"email": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Email address of the user, or an empty string when Immuta holds none.",
			},
		},
	}
}

func (d *globalUserDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureDataSourceClient(req, resp)
	requireRequestApp(d.client, "immuta_global_user", &resp.Diagnostics)
}

func (d *globalUserDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config globalUserDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	username := config.Username.ValueString()
	iamID := config.IAMID.ValueString()
	description := fmt.Sprintf("username %q", username)
	if iamID != "" {
		description += fmt.Sprintf(" in iam_id %q", iamID)
	}

	users, truncated, err := d.client.SearchRequestAppUsers(ctx, username)
	if err != nil {
		resp.Diagnostics.Append(requestAppErrorDiagnostic(fmt.Sprintf("Unable to look up the Immuta user with %s", description), err))

		return
	}

	matches := exactGlobalUserMatches(users, username, iamID)
	switch len(matches) {
	case 0:
		detail := fmt.Sprintf("No Immuta user has %s. The match is exact; check the value, and that the user has been "+
			"synchronised from its identity manager.", description)
		if truncated {
			detail += fmt.Sprintf(" Immuta's search returned its maximum of %d users that partially match, so the user "+
				"may exist but not have been returned; use a more specific username.", immutaapi.RequestAppUserSearchLimit)
		}
		resp.Diagnostics.AddError("No Immuta user found", detail)

		return
	case 1:
	default:
		var found []string
		for _, user := range matches {
			found = append(found, fmt.Sprintf("%s (iam_id %q)", user.Username, user.IAMID))
		}
		slices.Sort(found)
		resp.Diagnostics.AddError("Several Immuta users found",
			fmt.Sprintf("%d Immuta users have %s: %s. Set iam_id to choose one.", len(matches), description, strings.Join(found, ", ")))

		return
	}

	user := matches[0]
	config.ID = types.StringValue(user.GlobalUserID)
	config.GlobalUserID = types.StringValue(user.GlobalUserID)
	config.Username = types.StringValue(user.Username)
	config.IAMID = types.StringValue(user.IAMID)
	config.Name = types.StringValue(user.Name)
	config.Email = types.StringValue(user.Email)

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// exactGlobalUserMatches keeps the users whose username matches exactly, in
// the identity manager when one is given; Immuta's search matches partially.
// Users without a global user ID are unusable and dropped.
func exactGlobalUserMatches(users []immutaapi.RequestAppUser, username, iamID string) []immutaapi.RequestAppUser {
	return slices.DeleteFunc(slices.Clone(users), func(user immutaapi.RequestAppUser) bool {
		return user.GlobalUserID == "" || user.Username != username || (iamID != "" && user.IAMID != iamID)
	})
}
