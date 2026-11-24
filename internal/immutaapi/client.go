// Package immutaapi provides a small typed HTTP client for the documented
// Immuta SaaS Govern API.
//
// The client deliberately implements only documented endpoints and attributes.
// Undocumented endpoints are internal to Immuta and must not be used.
package immutaapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultRequestTimeout is the HTTP timeout applied when the caller does
	// not configure one.
	DefaultRequestTimeout = 30 * time.Second

	// DefaultRetryCount is the number of retries performed for idempotent
	// requests that fail with a transient error.
	DefaultRetryCount = 2

	// MaxRetryCount bounds the configurable retry count.
	MaxRetryCount = 5

	defaultUserAgent  = "terraform-provider-immuta"
	defaultRetryWait  = 500 * time.Millisecond
	maxResponseBytes  = 8 << 20
	maxErrorBodyBytes = 8 << 10
)

// Client is a typed client for the Immuta Govern API. It optionally holds a
// second client for the Request app (formerly Marketplace) API, which Immuta
// serves from a regional URL with its own credential.
type Client struct {
	baseURL *url.URL
	// credential is the secret sent with every request; it is redacted from
	// errors.
	credential string
	// authorization is the full Authorization header value.
	authorization string
	httpClient    *http.Client
	userAgent     string
	timeout       time.Duration
	retryCount    int
	retryWait     time.Duration

	requestAppURL   string
	requestAppToken string
	requestApp      *Client
}

// ErrRequestAppNotConfigured is returned by Request app API calls when the
// client was built without WithRequestApp.
var ErrRequestAppNotConfigured = errors.New("the Immuta Request app API is not configured: it needs its regional base URL and a personal access token")

// Option customises a Client.
type Option func(*Client)

// WithHTTPClient replaces the underlying HTTP client. The provided client is
// never mutated; a shallow copy is used when a request timeout is also set.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// WithRequestTimeout sets a finite timeout for every HTTP request.
func WithRequestTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		if timeout > 0 {
			c.timeout = timeout
		}
	}
}

// WithUserAgent sets the User-Agent header sent with every request.
func WithUserAgent(userAgent string) Option {
	return func(c *Client) {
		if strings.TrimSpace(userAgent) != "" {
			c.userAgent = userAgent
		}
	}
}

// WithRetryCount sets the number of retries for idempotent requests that fail
// with a transient error. The value is clamped to [0, MaxRetryCount].
func WithRetryCount(retries int) Option {
	return func(c *Client) {
		c.retryCount = retries
	}
}

// WithRequestApp configures the Request app (formerly Marketplace) API.
//
// As documented, it is served from a regional base URL, such as
// https://na.api.immutacloud.com/marketplace, and authenticated with a
// personal access token sent as a bearer token, not with the Govern API key.
// Request app calls fail with ErrRequestAppNotConfigured without it.
func WithRequestApp(baseURL, token string) Option {
	return func(c *Client) {
		c.requestAppURL = strings.TrimSpace(baseURL)
		c.requestAppToken = strings.TrimSpace(token)
	}
}

// WithRetryWait sets the base backoff between retries.
func WithRetryWait(wait time.Duration) Option {
	return func(c *Client) {
		if wait >= 0 {
			c.retryWait = wait
		}
	}
}

// NewClient returns a Client for the given Immuta host and API key.
//
// host accepts either a bare hostname ("example.immuta.com") or a full base
// URL ("https://example.immuta.com"). The API key is sent in the Authorization
// header, as documented for the Govern API key authentication method.
func NewClient(host, apiKey string, opts ...Option) (*Client, error) {
	baseURL, err := normalizeHost(host)
	if err != nil {
		return nil, err
	}

	key := strings.TrimSpace(apiKey)
	if key == "" {
		return nil, errors.New("an Immuta API key is required")
	}

	c := &Client{
		baseURL:       baseURL,
		credential:    key,
		authorization: key,
		userAgent:     defaultUserAgent,
		timeout:       DefaultRequestTimeout,
		retryCount:    DefaultRetryCount,
		retryWait:     defaultRetryWait,
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.retryCount < 0 {
		c.retryCount = 0
	}
	if c.retryCount > MaxRetryCount {
		c.retryCount = MaxRetryCount
	}

	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}
	if c.timeout > 0 {
		httpClient := *c.httpClient
		httpClient.Timeout = c.timeout
		c.httpClient = &httpClient
	}

	if c.requestAppURL != "" || c.requestAppToken != "" {
		requestApp, err := c.newRequestAppClient()
		if err != nil {
			return nil, err
		}
		c.requestApp = requestApp
	}

	return c, nil
}

// newRequestAppClient derives the Request app client: same transport,
// timeout and retries, but its own base URL and bearer credential.
func (c *Client) newRequestAppClient() (*Client, error) {
	if c.requestAppURL == "" {
		return nil, errors.New("a Request app API base URL is required with a Request app token")
	}
	if c.requestAppToken == "" {
		return nil, errors.New("a Request app personal access token is required with a Request app API base URL")
	}

	baseURL, err := normalizeHost(c.requestAppURL)
	if err != nil {
		return nil, fmt.Errorf("invalid Request app API base URL: %w", err)
	}

	requestApp := *c
	requestApp.baseURL = baseURL
	requestApp.credential = c.requestAppToken
	requestApp.authorization = "Bearer " + c.requestAppToken
	requestApp.requestAppURL = ""
	requestApp.requestAppToken = ""
	requestApp.requestApp = nil

	return &requestApp, nil
}

// HasRequestApp reports whether the Request app API is configured.
func (c *Client) HasRequestApp() bool {
	return c.requestApp != nil
}

// RequestAppURL returns the normalized Request app API base URL, or an empty
// string when it is not configured. It never contains credentials.
func (c *Client) RequestAppURL() string {
	if c.requestApp == nil {
		return ""
	}

	return c.requestApp.Host()
}

// requestAppClient returns the client for Request app API calls.
func (c *Client) requestAppClient() (*Client, error) {
	if c.requestApp == nil {
		return nil, ErrRequestAppNotConfigured
	}

	return c.requestApp, nil
}

// Host returns the normalized base URL the client talks to. It never contains
// credentials.
func (c *Client) Host() string {
	return c.baseURL.String()
}

func normalizeHost(host string) (*url.URL, error) {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return nil, errors.New("an Immuta host is required")
	}

	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid Immuta host %q: %w", host, err)
	}

	switch parsed.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("invalid Immuta host %q: scheme must be http or https", host)
	}

	if parsed.Host == "" {
		return nil, fmt.Errorf("invalid Immuta host %q: missing hostname", host)
	}

	// Credentials, queries and fragments are never part of the API base URL.
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	parsed.RawPath = ""

	return parsed, nil
}

type request struct {
	method string
	// segments are joined onto the base URL and escaped individually.
	segments []string
	query    url.Values
	body     any
}

// endpoint builds an absolute request URL, escaping each path segment
// individually.
//
// net/url's JoinPath treats its arguments as pre-escaped path segments and
// silently returns an empty path when a segment contains characters such as
// % or /, which would send the request to the wrong URL.
func (c *Client) endpoint(segments []string, query url.Values) *url.URL {
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		escaped = append(escaped, url.PathEscape(segment))
	}

	rawPath := c.baseURL.EscapedPath() + "/" + strings.Join(escaped, "/")

	endpoint := *c.baseURL
	endpoint.RawPath = rawPath
	endpoint.Path = rawPath
	if unescaped, err := url.PathUnescape(rawPath); err == nil {
		endpoint.Path = unescaped
	}
	if len(query) > 0 {
		endpoint.RawQuery = query.Encode()
	}

	return &endpoint
}

// do performs an API request and returns the raw response body. A nil body is
// returned for 204 responses.
func (c *Client) do(ctx context.Context, r request) ([]byte, error) {
	endpoint := c.endpoint(r.segments, r.query)

	var payload []byte
	if r.body != nil {
		encoded, err := json.Marshal(r.body)
		if err != nil {
			return nil, fmt.Errorf("encoding %s %s request body: %w", r.method, endpoint.Path, err)
		}
		payload = encoded
	}

	attempts := 1
	if isIdempotent(r.method) {
		attempts += c.retryCount
	}

	var lastErr error
	for attempt := range attempts {
		if attempt > 0 {
			if err := sleepWithContext(ctx, c.retryWait*time.Duration(attempt)); err != nil {
				return nil, err
			}
		}

		body, retryable, err := c.attempt(ctx, r, endpoint, payload)
		if err == nil {
			return body, nil
		}

		lastErr = err
		if !retryable {
			return nil, err
		}
	}

	return nil, lastErr
}

func (c *Client) attempt(ctx context.Context, r request, endpoint *url.URL, payload []byte) ([]byte, bool, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, r.method, endpoint.String(), body)
	if err != nil {
		return nil, false, fmt.Errorf("creating %s %s request: %w", r.method, endpoint.Path, err)
	}

	// The documented Govern API key method passes the key directly in
	// Authorization; the Request app takes its token as a bearer token.
	req.Header.Set("Authorization", c.authorization)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, fmt.Errorf("%s %s: %w", r.method, endpoint.Path, ctxErr)
		}
		return nil, true, fmt.Errorf("%s %s: %s", r.method, endpoint.Path, redact(err.Error(), c.credential))
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, isRetryableStatus(resp.StatusCode), c.newAPIError(r.method, endpoint.Path, resp)
	}

	if resp.StatusCode == http.StatusNoContent {
		return nil, false, nil
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, fmt.Errorf("%s %s: %w", r.method, endpoint.Path, ctxErr)
		}
		return nil, true, fmt.Errorf("reading %s %s response: %s", r.method, endpoint.Path, redact(err.Error(), c.credential))
	}

	return raw, false, nil
}

func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

// isRetryableStatus reports whether a status code represents a transient
// failure that is safe to retry for an idempotent request.
func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
