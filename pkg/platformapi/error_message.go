package platformapi

import (
	"encoding/json"
	"net/http"
	"unicode/utf8"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const maxClassifiedBodyBytes = 64 * 1024

// reasonLabels is intentionally a closed allowlist; StatusReason is a string
// type, not a runtime-validated enum. Never display the raw reason or message.
var reasonLabels = map[metav1.StatusReason]string{
	metav1.StatusReasonNotFound:           "not found",
	metav1.StatusReasonAlreadyExists:      "already exists",
	metav1.StatusReasonConflict:           "conflict",
	metav1.StatusReasonInvalid:            "invalid request",
	metav1.StatusReasonBadRequest:         "invalid request",
	metav1.StatusReasonTooManyRequests:    "too many requests",
	metav1.StatusReasonTimeout:            "request timed out",
	metav1.StatusReasonServerTimeout:      "request timed out",
	metav1.StatusReasonServiceUnavailable: "service unavailable",
	metav1.StatusReasonInternalError:      "server error",
}

// Generic tokens are recognized only with their matching HTTP status. This
// prevents a contradictory body from overriding the response's classification.
var genericErrorLabels = map[string]struct {
	statusCode int
	label      string
}{
	"unauthorized":        {http.StatusUnauthorized, "not authenticated"},
	"forbidden":           {http.StatusForbidden, "permission denied"},
	"not_found":           {http.StatusNotFound, "not found"},
	"already_exists":      {http.StatusConflict, "already exists"},
	"conflict":            {http.StatusConflict, "conflict"},
	"bad_request":         {http.StatusBadRequest, "invalid request"},
	"invalid_request":     {http.StatusBadRequest, "invalid request"},
	"too_many_requests":   {http.StatusTooManyRequests, "too many requests"},
	"internal_error":      {http.StatusInternalServerError, "server error"},
	"service_unavailable": {http.StatusServiceUnavailable, "service unavailable"},
	"timeout":             {http.StatusGatewayTimeout, "request timed out"},
}

// displayMessage returns only a fixed label. A decoded Status reason can be
// used even when its raw body exceeds 64 KiB; raw-body parsing itself is capped.
func displayMessage(statusCode int, status *metav1.Status, body []byte) string {
	switch statusCode {
	case http.StatusUnauthorized:
		return "not authenticated"
	case http.StatusForbidden:
		return "permission denied"
	}

	if status != nil {
		if label, ok := reasonLabels[status.Reason]; ok {
			return label
		}
	}

	if len(body) > 0 && len(body) <= maxClassifiedBodyBytes && utf8.Valid(body) {
		if label := labelFromRawBody(statusCode, body); label != "" {
			return label
		}
	}
	return statusFallback(statusCode)
}

func labelFromRawBody(statusCode int, body []byte) string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return ""
	}

	// A raw Kubernetes Status must identify itself as a failed Status object.
	// Already-decoded Status values reach displayMessage through its status arg.
	var status metav1.Status
	if json.Unmarshal(body, &status) == nil && status.Kind == "Status" && status.Status == metav1.StatusFailure {
		if label, ok := reasonLabels[status.Reason]; ok {
			return label
		}
		return ""
	}

	// Gecko's generic envelope has a top-level string token and optionally a
	// message. Reject other shapes instead of guessing from nested fields.
	if len(fields) < 1 || len(fields) > 2 {
		return ""
	}
	for key := range fields {
		if key != "error" && key != "message" {
			return ""
		}
	}
	if rawMessage, ok := fields["message"]; ok {
		var message string
		if json.Unmarshal(rawMessage, &message) != nil {
			return ""
		}
	}
	var token string
	if json.Unmarshal(fields["error"], &token) != nil {
		return ""
	}
	if known, ok := genericErrorLabels[token]; ok && known.statusCode == statusCode {
		return known.label
	}
	return ""
}

func statusFallback(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "invalid request"
	case http.StatusNotFound:
		return "not found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusTooManyRequests:
		return "too many requests"
	case http.StatusInternalServerError:
		return "server error"
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return "service unavailable"
	case http.StatusGatewayTimeout:
		return "request timed out"
	default:
		return "request failed"
	}
}
