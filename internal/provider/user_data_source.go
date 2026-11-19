package provider

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var (
	_ datasource.DataSource              = (*userDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*userDataSource)(nil)
)

// NewUserDataSource returns the immuta_user data source.
func NewUserDataSource() datasource.DataSource {
	return &userDataSource{}
}

type userDataSource struct {
	client *immutaapi.Client
}

type userDataSourceModel struct {
	ID       types.String `tfsdk:"id"`
	IAMID    types.String `tfsdk:"iam_id"`
	UserID   types.String `tfsdk:"user_id"`
	Email    types.String `tfsdk:"email"`
	Name     types.String `tfsdk:"name"`
	Disabled types.Bool   `tfsdk:"disabled"`
}

func (d *userDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (d *userDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	lookup := []validator.String{
		stringvalidator.LengthAtLeast(1),
		stringvalidator.ExactlyOneOf(path.MatchRoot("email"), path.MatchRoot("user_id")),
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing Immuta user, in any identity manager, by email address or username, with the " +
			"documented `GET /bim/user` search. Use it to reference users Terraform does not manage, such as users " +
			"synchronised from an external identity manager.\n\n" +
			"Exactly one user must match: the match is exact (case-insensitive for email), and an error explains " +
			"what to do when no user or several users match.",
		Attributes: map[string]schema.Attribute{
			"email": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Email address of the user to look up. Exactly one of `email` and `user_id` is required.",
				Validators:          lookup,
			},
			"user_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Username of the user to look up. Exactly one of `email` and `user_id` is required.",
				Validators:          lookup,
			},
			"iam_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Identity manager to look in, for example `bim`. Needed when the same person exists in several identity managers.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Numeric profile ID of the user, the same identifier as `immuta_user.id`. Use `tonumber()` " +
					"where a JSON or YAML document needs it as a number.",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Display name of the user.",
			},
			"disabled": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the user is disabled. Immuta's search only returns enabled users, so this is `false`.",
			},
		},
	}
}

func (d *userDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureDataSourceClient(req, resp)
}

func (d *userDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config userDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := immutaapi.SearchUsersOptions{
		Email:  config.Email.ValueString(),
		UserID: config.UserID.ValueString(),
		IAMID:  config.IAMID.ValueString(),
	}
	description := describeUserLookup(opts)

	users, err := d.client.SearchUsers(ctx, opts)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Unable to look up the Immuta user with %s", description), err))

		return
	}

	matches := exactUserMatches(users, opts)
	switch len(matches) {
	case 0:
		resp.Diagnostics.AddError("No Immuta user found",
			fmt.Sprintf("No enabled Immuta user has %s. The match is exact; check the value, and that the user has been "+
				"synchronised from its identity manager.", description))

		return
	case 1:
	default:
		slices.SortFunc(matches, func(a, b immutaapi.User) int {
			return strings.Compare(a.IAMID+"/"+a.UserID, b.IAMID+"/"+b.UserID)
		})
		var found []string
		for _, user := range matches {
			found = append(found, fmt.Sprintf("%s (iam_id %q)", user.UserID, user.IAMID))
		}
		resp.Diagnostics.AddError("Several Immuta users found",
			fmt.Sprintf("%d Immuta users have %s: %s. Set iam_id, or look the user up by user_id, to choose one.",
				len(matches), description, strings.Join(found, ", ")))

		return
	}

	user := matches[0]
	config.ID = types.StringValue(strconv.FormatInt(user.ID, 10))
	config.IAMID = types.StringValue(user.IAMID)
	config.UserID = types.StringValue(user.UserID)
	config.Email = types.StringValue(userEmail(user))
	config.Name = types.StringValue(userName(user))
	config.Disabled = types.BoolValue(user.Disabled)

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// exactUserMatches keeps the users matching the lookup exactly; Immuta's
// search matches partially. Email addresses are compared case-insensitively.
func exactUserMatches(users []immutaapi.User, opts immutaapi.SearchUsersOptions) []immutaapi.User {
	return slices.DeleteFunc(slices.Clone(users), func(user immutaapi.User) bool {
		switch {
		case user.ID == 0:
			return true
		case opts.IAMID != "" && user.IAMID != opts.IAMID:
			return true
		case opts.Email != "":
			return !strings.EqualFold(userEmail(user), opts.Email)
		default:
			return user.UserID != opts.UserID
		}
	})
}

func describeUserLookup(opts immutaapi.SearchUsersOptions) string {
	description := fmt.Sprintf("user_id %q", opts.UserID)
	if opts.Email != "" {
		description = fmt.Sprintf("email %q", opts.Email)
	}
	if opts.IAMID != "" {
		description += fmt.Sprintf(" in iam_id %q", opts.IAMID)
	}

	return description
}

func userEmail(user immutaapi.User) string {
	if user.Profile == nil {
		return ""
	}

	return user.Profile.Email
}

func userName(user immutaapi.User) string {
	if user.Profile == nil {
		return ""
	}

	return user.Profile.Name
}
