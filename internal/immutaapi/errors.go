package immutaapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

// ErrNotFound is returned when Immuta reports that an object does not exist.
// Compare with errors.Is or IsNotFound.
var ErrNotFound = errors.New("immuta: object not found")

const maxErrorMessageRunes = 256

// APIError describes a non-2xx response from the Immuta API.
//
// Only the request method, request path, status code and a short server
// message are retained. Request bodies, response payloads and authorization
// headers are never captured, so an APIError is safe to surface in Terraform
// diagnostics.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("immuta API %s %s returned HTTP %d %s", e.Method, e.Path, e.StatusCode, http.StatusText(e.StatusCode))
	}

	return fmt.Sprintf("immuta API %s %s returned HTTP %d %s: %s", e.Method, e.Path, e.StatusCode, http.StatusText(e.StatusCode), e.Message)
}

// Is allows errors.Is(err, ErrNotFound) to match documented 404 responses.
func (e *APIError) Is(target error) bool {
	return target == ErrNotFound && e.StatusCode == http.StatusNotFound
}

// IsNotFound reports whether err represents a documented not-found response.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

func (c *Client) newAPIError(method, path string, resp *http.Response) *APIError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))

	return &APIError{
		StatusCode: resp.StatusCode,
		Method:     method,
		Path:       path,
		Message:    redact(errorMessage(body), c.credential),
	}
}

// errorMessage extracts a short, human readable message from an error
// response. JSON payloads with an unrecognised shape are not echoed back, to
// avoid surfacing complete API responses.
func errorMessage(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}

	var payload struct {
		Message string `json:"message"`
		Error   string `json:"error"`
		Detail  string `json:"detail"`
		// The connections API returns a list of errors instead.
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		for _, candidate := range []string{payload.Message, payload.Error, payload.Detail} {
			if candidate != "" {
				return truncate(collapseWhitespace(candidate))
			}
		}

		messages := make([]string, 0, len(payload.Errors))
		for _, item := range payload.Errors {
			switch {
			case item.Code != "" && item.Message != "":
				messages = append(messages, item.Code+": "+item.Message)
			case item.Message != "":
				messages = append(messages, item.Message)
			}
		}

		return truncate(collapseWhitespace(strings.Join(messages, "; ")))
	}

	return truncate(collapseWhitespace(trimmed))
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string) string {
	if utf8.RuneCountInString(s) <= maxErrorMessageRunes {
		return s
	}

	return string([]rune(s)[:maxErrorMessageRunes]) + "..."
}

// redact removes the API key from a string as a defence in depth measure, in
// case a server or transport error echoes the credential back.
func redact(s, secret string) string {
	if s == "" || secret == "" {
		return s
	}

	return strings.ReplaceAll(s, secret, "<redacted>")
}
