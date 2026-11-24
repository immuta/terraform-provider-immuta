package provider

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

// configureClient pulls the shared API client out of the provider data. It
// returns nil before the provider is configured, which the framework handles
// by calling Configure again later.
func configureClient(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *immutaapi.Client {
	return providerClient(req.ProviderData, &resp.Diagnostics)
}

// configureDataSourceClient is configureClient for data sources.
func configureDataSourceClient(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *immutaapi.Client {
	return providerClient(req.ProviderData, &resp.Diagnostics)
}

func providerClient(providerData any, diagnostics *diag.Diagnostics) *immutaapi.Client {
	if providerData == nil {
		return nil
	}

	client, ok := providerData.(*immutaapi.Client)
	if !ok {
		diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *immutaapi.Client, got %T. This is a bug in the provider.", providerData),
		)

		return nil
	}

	return client
}

// isSet reports whether an optional attribute holds a usable value.
func isSet(value types.String) bool {
	return !value.IsNull() && !value.IsUnknown()
}

// parseInt64Attribute reads a numeric identifier that is held in state as a
// string. A malformed value means the state was hand-edited or imported with
// the wrong ID format, so it is reported against the attribute itself.
func parseInt64Attribute(value types.String, attribute path.Path, kind string, diagnostics *diag.Diagnostics) (int64, bool) {
	parsed, err := strconv.ParseInt(value.ValueString(), 10, 64)
	if err != nil {
		diagnostics.AddAttributeError(
			attribute,
			fmt.Sprintf("Invalid Immuta %s ID in state", kind),
			fmt.Sprintf("Expected a numeric %s ID, got %q. Remove the resource from state and import it again.", kind, value.ValueString()),
		)

		return 0, false
	}

	return parsed, true
}

// bimErrorDiagnostic turns an error from the BIM endpoints into an actionable
// diagnostic. API errors never contain credentials or full response payloads.
func bimErrorDiagnostic(summary string, err error, conflictHint string) diag.Diagnostic {
	detail := err.Error()

	var apiErr *immutaapi.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			detail += "\n\nCheck that the configured API key is valid and that the Immuta user holds the USER_ADMIN permission required by the identity endpoints."
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			detail += "\n\nImmuta rejected the request as invalid. Check the configured attribute values."
		case http.StatusConflict:
			detail += "\n\n" + conflictHint
		}
	}

	return diag.NewErrorDiagnostic(summary, detail)
}

func userErrorDiagnostic(summary string, err error) diag.Diagnostic {
	return bimErrorDiagnostic(summary, err, "Immuta reported a conflict. A user with the same username may already exist in this identity manager.")
}

func groupErrorDiagnostic(summary string, err error) diag.Diagnostic {
	return bimErrorDiagnostic(summary, err, "Immuta reported a conflict. A group with the same name may already exist in this identity manager.")
}

func membershipErrorDiagnostic(summary string, err error) diag.Diagnostic {
	return bimErrorDiagnostic(summary, err, "Immuta reported a conflict. The user may already be a member of this group, including through a membership created outside Terraform.")
}

func attributeErrorDiagnostic(summary string, err error) diag.Diagnostic {
	return bimErrorDiagnostic(summary, err, "Immuta reported a conflict. The attribute may already be assigned.")
}

func tagErrorDiagnostic(summary string, err error) diag.Diagnostic {
	return bimErrorDiagnostic(summary, err, "Immuta reported a conflict. A tag with the same name may already exist.")
}

// tagDeleteErrorDiagnostic explains why Immuta refuses to delete a tag that is
// still used by a framework, rather than suggesting the configuration is
// invalid.
func tagDeleteErrorDiagnostic(summary string, err error) diag.Diagnostic {
	var apiErr *immutaapi.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(apiErr.Message), "framework") {
		return diag.NewErrorDiagnostic(
			summary,
			err.Error()+"\n\nImmuta does not delete a tag while a framework uses it. Delete or change the framework first. "+
				"When both are managed by Terraform, reference immuta_tag.<name>.name from the immuta_framework so "+
				"that Terraform removes the framework before the tag. A framework created outside Terraform, or left "+
				"behind by a failed apply, must be deleted or imported first.",
		)
	}

	return tagErrorDiagnostic(summary, err)
}

// identifierErrorDiagnostic adds a pointer to immuta_tag, because the most
// common rejection is a tag that does not exist yet.
func identifierErrorDiagnostic(summary string, err error) diag.Diagnostic {
	diagnostic := bimErrorDiagnostic(summary, err, "Immuta reported a conflict. An identifier with the same name may already exist in this scope.")

	var apiErr *immutaapi.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(apiErr.Message), "tag") {
		return diag.NewErrorDiagnostic(
			summary,
			diagnostic.Detail()+"\n\nEvery tag an identifier applies must already exist in Immuta. Create it with an immuta_tag resource and reference immuta_tag.<name>.name so that Terraform creates the tag first.",
		)
	}

	return diagnostic
}
