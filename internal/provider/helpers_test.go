package provider

import (
	"net/http"
	"strings"
	"testing"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

func TestTagDeleteErrorDiagnostic(t *testing.T) {
	t.Parallel()

	// The message a real tenant returns when a framework still uses the tag.
	inUse := &immutaapi.APIError{
		StatusCode: http.StatusBadRequest,
		Method:     http.MethodDelete,
		Path:       "/tag/HR Framework.Internal Employee Data",
		Message:    "Tags associated with frameworks cannot be deleted",
	}

	detail := tagDeleteErrorDiagnostic("Unable to delete the Immuta tag", inUse).Detail()
	if !strings.Contains(detail, "Tags associated with frameworks cannot be deleted") {
		t.Errorf("detail %q should keep Immuta's message", detail)
	}
	if !strings.Contains(detail, "reference immuta_tag.<name>.name from the immuta_framework") {
		t.Errorf("detail %q should explain how to order the destroy", detail)
	}
	if strings.Contains(detail, "Check the configured attribute values") {
		t.Errorf("detail %q should not blame the configuration", detail)
	}

	// Other rejections keep the existing tag diagnostic.
	other := &immutaapi.APIError{StatusCode: http.StatusBadRequest, Method: http.MethodDelete, Path: "/tag/x", Message: "bad name"}
	if got, want := tagDeleteErrorDiagnostic("s", other).Detail(), tagErrorDiagnostic("s", other).Detail(); got != want {
		t.Errorf("detail = %q, want the generic tag diagnostic %q", got, want)
	}
}
