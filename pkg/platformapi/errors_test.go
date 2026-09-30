package platformapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAuthenticationAndAuthorizationLabels(t *testing.T) {
	secret := "Bearer secret-token alice@example.com https://user:password@example.com"
	for _, tc := range []struct {
		name       string
		statusCode int
		status     *metav1.Status
		body       []byte
		want       string
	}{
		{"401 empty", 401, nil, nil, "not authenticated"},
		{"401 gecko", 401, nil, []byte(`{"error":"unauthorized","message":"` + secret + `"}`), "not authenticated"},
		{"401 status", 401, &metav1.Status{Reason: metav1.StatusReasonForbidden, Message: secret}, nil, "not authenticated"},
		{"401 contradictory", 401, nil, []byte(`{"error":"forbidden"}`), "not authenticated"},
		{"403 empty", 403, nil, nil, "permission denied"},
		{"403 gecko", 403, nil, []byte(`{"error":"forbidden"}`), "permission denied"},
		{"403 status", 403, &metav1.Status{Reason: metav1.StatusReasonUnauthorized, Message: secret}, nil, "permission denied"},
		{"403 contradictory", 403, nil, []byte(`{"error":"unauthorized"}`), "permission denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := newHTTPError("GET", "clusters", "alice@example.com", tc.statusCode, tc.status, tc.body)
			if got := err.Error(); got != tc.want {
				t.Fatalf("Error() = %q, want %q", got, tc.want)
			}
			if strings.Contains(err.Error(), "alice@example.com") || strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "secret-token") {
				t.Errorf("unsafe error text: %q", err.Error())
			}
		})
	}
}

func TestStatusReasonLabels(t *testing.T) {
	for _, tc := range []struct {
		reason metav1.StatusReason
		want   string
	}{
		{metav1.StatusReasonNotFound, "not found"},
		{metav1.StatusReasonAlreadyExists, "already exists"},
		{metav1.StatusReasonConflict, "conflict"},
		{metav1.StatusReasonInvalid, "invalid request"},
		{metav1.StatusReasonBadRequest, "invalid request"},
		{metav1.StatusReasonTooManyRequests, "too many requests"},
		{metav1.StatusReasonTimeout, "request timed out"},
		{metav1.StatusReasonServerTimeout, "request timed out"},
		{metav1.StatusReasonServiceUnavailable, "service unavailable"},
		{metav1.StatusReasonInternalError, "server error"},
	} {
		t.Run(string(tc.reason), func(t *testing.T) {
			status := &metav1.Status{Reason: tc.reason, Message: "Bearer secret-token"}
			if got := displayMessage(418, status, nil); got != tc.want {
				t.Errorf("displayMessage() = %q, want %q", got, tc.want)
			}
			body := []byte(fmt.Sprintf(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":%q,"message":"Bearer secret-token"}`, tc.reason))
			if got := displayMessage(418, nil, body); got != tc.want {
				t.Errorf("raw Status = %q, want %q", got, tc.want)
			}
		})
	}
	for _, reason := range []metav1.StatusReason{"", "Bearer secret-token"} {
		if got := displayMessage(409, &metav1.Status{Reason: reason}, nil); got != "conflict" {
			t.Errorf("unknown reason %q returned %q", reason, got)
		}
	}
}

func TestGenericBodyLabels(t *testing.T) {
	for _, tc := range []struct {
		name       string
		statusCode int
		body       string
		want       string
	}{
		{"known forbidden", 403, `{"error":"forbidden"}`, "permission denied"},
		{"known unauthorized", 401, `{"error":"unauthorized"}`, "not authenticated"},
		{"known already exists", 409, `{"error":"already_exists","message":"Bearer secret-token"}`, "already exists"},
		{"unknown token", 409, `{"error":"Bearer secret-token"}`, "conflict"},
		{"contradictory token", 500, `{"error":"forbidden"}`, "server error"},
		{"arbitrary message", 500, `{"message":"Bearer secret-token"}`, "server error"},
		{"nested only", 500, `{"outer":{"error":"forbidden"}}`, "server error"},
		{"non-string error", 500, `{"error":{"value":"forbidden"}}`, "server error"},
		{"array error", 500, `{"error":["forbidden"]}`, "server error"},
		{"unknown extra key", 409, `{"error":"already_exists","claims":"secret"}`, "conflict"},
		{"wrong message shape", 409, `{"error":"already_exists","message":{"token":"secret"}}`, "conflict"},
		{"invalid Status shape", 409, `{"kind":"Status","status":"Failure","reason":"AlreadyExists","details":"not an object"}`, "conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := displayMessage(tc.statusCode, nil, []byte(tc.body)); got != tc.want {
				t.Errorf("displayMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBadBodiesAndFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name       string
		statusCode int
		body       []byte
		want       string
	}{
		{"empty", 400, nil, "invalid request"},
		{"malformed", 404, []byte(`{"error":`), "not found"},
		{"text", 409, []byte("Bearer secret-token"), "conflict"},
		{"array", 429, []byte(`["forbidden"]`), "too many requests"},
		{"invalid UTF-8", 500, []byte{'{', '"', 'e', 'r', 'r', 'o', 'r', '"', ':', '"', 0xff, '"', '}'}, "server error"},
		{"unknown status", 418, []byte(`{"error":"unknown"}`), "request failed"},
		{"unprocessable", 422, nil, "invalid request"},
		{"bad gateway", 502, nil, "service unavailable"},
		{"service unavailable", 503, nil, "service unavailable"},
		{"gateway timeout", 504, nil, "request timed out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := displayMessage(tc.statusCode, nil, tc.body); got != tc.want {
				t.Errorf("displayMessage() = %q, want %q", got, tc.want)
			}
		})
	}

	prefix, suffix := `{"error":"already_exists","message":"`, `"}`
	atLimit := []byte(prefix + strings.Repeat("x", maxClassifiedBodyBytes-len(prefix)-len(suffix)) + suffix)
	if got := displayMessage(409, nil, atLimit); got != "already exists" {
		t.Errorf("raw body at limit = %q, want already exists", got)
	}
	large := append(append([]byte(nil), atLimit[:len(atLimit)-len(suffix)]...), 'x')
	large = append(large, suffix...)
	if got := displayMessage(409, nil, large); got != "conflict" {
		t.Errorf("oversized raw body = %q, want conflict", got)
	}
	if got := displayMessage(409, &metav1.Status{Reason: metav1.StatusReasonAlreadyExists}, large); got != "already exists" {
		t.Errorf("oversized body with decoded Status = %q, want already exists", got)
	}
	if got := displayMessage(504, nil, nil); got != "request timed out" {
		t.Errorf("HTTP 504 = %q, want request timed out", got)
	}
}

func TestNoFreeFormServerText(t *testing.T) {
	secret := "Authorization: Bearer abc123 alice@example.com https://user:pass@example.com"
	status := &metav1.Status{
		Reason:  metav1.StatusReasonInvalid,
		Message: secret,
		Details: &metav1.StatusDetails{
			Name:   secret,
			Causes: []metav1.StatusCause{{Message: secret, Field: secret}},
		},
	}
	for _, body := range [][]byte{
		[]byte(secret),
		[]byte(fmt.Sprintf(`{"error":"unknown","message":%q}`, secret)),
		[]byte(fmt.Sprintf(`{"kind":"Status","status":"Failure","reason":"Invalid","message":%q,"details":{"name":%q}}`, secret, secret)),
	} {
		err := newHTTPError("POST", "clusters", "example", 400, status, body)
		if got := err.Error(); got != "invalid request" || strings.Contains(got, secret) {
			t.Errorf("unsafe error text: %q", got)
		}
	}
}

func TestHTTPErrorMetadataAndWrapping(t *testing.T) {
	name := strings.Repeat("n", maxStoredResourceNameBytes+40)
	err := newHTTPError("GET", "clusters", name, http.StatusForbidden, nil, []byte(`{"error":"forbidden"}`))
	var wrapped error = fmt.Errorf("listing clusters: %w", err)
	var got *HTTPError
	if !errors.As(wrapped, &got) || got != err {
		t.Fatal("errors.As did not recover HTTPError")
	}
	if got.StatusCode() != 403 || got.Method() != "GET" || got.Resource() != "clusters" || got.Name() != name[:maxStoredResourceNameBytes] {
		t.Errorf("unexpected metadata: %#v", got)
	}
	for _, forbidden := range []string{"403", "Platform API", "GET clusters", name} {
		if strings.Contains(err.Error(), forbidden) {
			t.Errorf("Error() includes %q: %q", forbidden, err.Error())
		}
	}
}

type timeoutFailure struct{ text string }

func (e timeoutFailure) Error() string { return e.text }
func (e timeoutFailure) Timeout() bool { return true }

func TestTransportErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		want  string
		is    error
	}{
		{"deadline", fmt.Errorf("https://user:pass@example.com: %w", context.DeadlineExceeded), "request timed out", context.DeadlineExceeded},
		{"cancellation", fmt.Errorf("Bearer token: %w", context.Canceled), "request canceled", context.Canceled},
		{"timeout interface", fmt.Errorf("wrapped: %w", timeoutFailure{"https://user:pass@example.com"}), "request timed out", context.DeadlineExceeded},
		{"connection", errors.New("dial https://user:pass@example.com failed"), "request failed", nil},
		{"token source", errors.New("obtaining auth token: Bearer secret-token"), "request failed", nil},
		{"unsupported content type", errors.New("HTTP 403 application/xml: <error>secret</error>"), "request failed", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := newTransportError(tc.cause)
			if err.Error() != tc.want {
				t.Errorf("Error() = %q, want %q", err.Error(), tc.want)
			}
			if tc.is != nil && !errors.Is(fmt.Errorf("operation: %w", err), tc.is) {
				t.Errorf("errors.Is did not find %v", tc.is)
			}
			if tc.is == nil && err.Unwrap() != nil {
				t.Errorf("generic error unexpectedly unwraps: %v", err.Unwrap())
			}
			for _, forbidden := range []string{"https://", "Bearer", "secret", "403", "application/xml"} {
				if strings.Contains(err.Error(), forbidden) {
					t.Errorf("Error() includes %q", forbidden)
				}
			}
			var got *TransportError
			if !errors.As(fmt.Errorf("operation: %w", err), &got) || got != err {
				t.Fatal("errors.As did not recover TransportError")
			}
		})
	}
}
