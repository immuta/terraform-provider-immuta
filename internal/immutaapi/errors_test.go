package immutaapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestErrorMessage(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		want string
	}{
		"message field":                {body: `{"message":"domain not found"}`, want: "domain not found"},
		"error field":                  {body: `{"error":"bad request"}`, want: "bad request"},
		"detail field":                 {body: `{"detail":"invalid type"}`, want: "invalid type"},
		"message wins over error":      {body: `{"message":"first","error":"second"}`, want: "first"},
		"unknown json shape is hidden": {body: `{"token":"secret","rows":[1,2,3]}`, want: ""},
		"plain text is kept":           {body: "service unavailable", want: "service unavailable"},
		"whitespace is collapsed":      {body: "service\n\tunavailable", want: "service unavailable"},
		"empty body":                   {body: "   ", want: ""},
		"connections api errors": {
			body: `{"errors":[{"code":"CONN002","message":"Connection already exists"},{"message":"second"}]}`,
			want: "CONN002: Connection already exists; second",
		},
		"connections api error details are hidden": {
			body: `{"errors":[{"code":"CONN001","message":"Invalid credential","details":{"password":"secret"}}]}`,
			want: "CONN001: Invalid credential",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := errorMessage([]byte(test.body)); got != test.want {
				t.Errorf("errorMessage(%q) = %q, want %q", test.body, got, test.want)
			}
		})
	}
}

func TestErrorMessageIsTruncated(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("a", 5000)

	got := errorMessage([]byte(body))
	if len(got) > maxErrorMessageRunes+len("...") {
		t.Errorf("errorMessage returned %d characters, want it to be truncated", len(got))
	}

	if !strings.HasSuffix(got, "...") {
		t.Error("a truncated message should end with an ellipsis")
	}
}

func TestAPIErrorMessageFormatting(t *testing.T) {
	t.Parallel()

	withMessage := &APIError{StatusCode: http.StatusBadRequest, Method: http.MethodPost, Path: "/domain", Message: "name is required"}
	if got, want := withMessage.Error(), "immuta API POST /domain returned HTTP 400 Bad Request: name is required"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	withoutMessage := &APIError{StatusCode: http.StatusInternalServerError, Method: http.MethodGet, Path: "/domain/abc"}
	if got, want := withoutMessage.Error(), "immuta API GET /domain/abc returned HTTP 500 Internal Server Error"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestAPIErrorNotFoundMatching(t *testing.T) {
	t.Parallel()

	notFound := &APIError{StatusCode: http.StatusNotFound, Method: http.MethodGet, Path: "/domain/abc"}
	if !IsNotFound(notFound) {
		t.Error("a 404 should match ErrNotFound")
	}

	if !IsNotFound(fmt.Errorf("reading the domain: %w", notFound)) {
		t.Error("a wrapped 404 should match ErrNotFound")
	}

	forbidden := &APIError{StatusCode: http.StatusForbidden, Method: http.MethodGet, Path: "/domain/abc"}
	if IsNotFound(forbidden) {
		t.Error("a 403 should not match ErrNotFound")
	}

	if IsNotFound(errors.New("some other failure")) {
		t.Error("an unrelated error should not match ErrNotFound")
	}

	if !IsNotFound(ErrNotFound) {
		t.Error("ErrNotFound should match itself")
	}
}

func TestRedact(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		value  string
		secret string
		want   string
	}{
		"secret is replaced":     {value: "token abc123 rejected", secret: "abc123", want: "token <redacted> rejected"},
		"empty secret is a noop": {value: "token abc123 rejected", secret: "", want: "token abc123 rejected"},
		"empty value is a noop":  {value: "", secret: "abc123", want: ""},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := redact(test.value, test.secret); got != test.want {
				t.Errorf("redact() = %q, want %q", got, test.want)
			}
		})
	}
}
