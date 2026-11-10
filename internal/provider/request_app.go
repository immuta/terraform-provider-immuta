package provider

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

// requestAppUnauthorizedHint explains a 401 from the Request app API.
const requestAppUnauthorizedHint = "The Request app API did not accept the personal access token. Check that `request_app_token` " +
	"is a valid, unexpired Immuta personal access token (not the Govern `api_key`), and that `request_app_url` " +
	"is the regional URL of the tenant's region: a token used in the wrong region is reported as not authenticated."

// requireRequestApp reports an error when a resource or data source that uses
// the Request app API is configured without it, before any request is made.
// A nil client means the provider is not configured yet, which is not an
// error.
func requireRequestApp(client *immutaapi.Client, typeName string, diagnostics *diag.Diagnostics) {
	if client == nil || client.HasRequestApp() {
		return
	}

	diagnostics.AddError(
		"Immuta Request app API not configured",
		fmt.Sprintf("%s uses the Request app (formerly Marketplace) API, which needs its own regional URL and "+
			"personal access token. Set the provider `request_app_url` and `request_app_token` arguments, or the %s and %s "+
			"environment variables. The URL is https://na.api.immutacloud.com/marketplace, "+
			"https://eu.api.immutacloud.com/marketplace or https://ap.api.immutacloud.com/marketplace, matching the tenant's region.",
			typeName, EnvRequestAppURL, EnvRequestAppToken),
	)
}

// requestAppErrorDiagnostic turns an error from a Request app API lookup into
// an actionable diagnostic. API errors never contain credentials.
func requestAppErrorDiagnostic(summary string, err error) diag.Diagnostic {
	detail := err.Error()

	var apiErr *immutaapi.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized:
			detail += "\n\n" + requestAppUnauthorizedHint
		case http.StatusForbidden:
			detail += "\n\nCheck that the Immuta user who owns the personal access token is allowed to search users and request forms in the Request app."
		}
	}

	return diag.NewErrorDiagnostic(summary, detail)
}
