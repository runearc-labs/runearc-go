package runearc

import (
	"fmt"
	"net/http"
	"time"
)

// Kind groups errors by what went wrong. API errors get the kind of their HTTP
// status; failures before a response arrives get one of the client kinds.
type Kind string

// The error kinds.
const (
	// KindInvalid is a 400, or another 4xx the API sends for a bad request.
	KindInvalid Kind = "invalid"
	// KindUnauthorized is a 401: the API key is missing, wrong or revoked.
	KindUnauthorized Kind = "unauthorized"
	// KindForbidden is a 403: the key may not do this.
	KindForbidden Kind = "forbidden"
	// KindNotFound is a 404.
	KindNotFound Kind = "not_found"
	// KindConflict is a 409, such as a reused idempotency key with another body.
	KindConflict Kind = "conflict"
	// KindLimitReached is a 402: the plan's limit is used up.
	KindLimitReached Kind = "limit_reached"
	// KindRateLimited is a 429.
	KindRateLimited Kind = "rate_limited"
	// KindUnavailable is a 503.
	KindUnavailable Kind = "unavailable"
	// KindInternal is any other 5xx.
	KindInternal Kind = "internal"
	// KindNetwork means the API could not be reached.
	KindNetwork Kind = "network"
	// KindTimeout means an attempt got no response in time.
	KindTimeout Kind = "timeout"
	// KindCanceled means the context was canceled or its deadline passed.
	KindCanceled Kind = "canceled"
	// KindClient is a problem on the caller's side before any request: a
	// missing API key, or a response that is not JSON.
	KindClient Kind = "client"
	// KindWebhook means a webhook payload failed verification.
	KindWebhook Kind = "webhook"
)

// Error is every failure the SDK reports. Use [errors.As] to get it:
//
//	var apiErr *runearc.Error
//	if errors.As(err, &apiErr) && apiErr.Code == "sender_not_verified" {
//		log.Println(apiErr.Fix)
//	}
//
// API errors carry the API's Code, Message and Fix. Failures before a response
// arrives have Status 0 and the codes network_error, timeout, aborted,
// missing_api_key or invalid_response. Webhook verification failures have
// Status 0 and Kind [KindWebhook].
type Error struct {
	// Status is the HTTP status, or 0 when no response arrived.
	Status int
	// Code is a stable machine readable code, such as invalid_from or rate_limited.
	Code string
	// Kind groups the error by cause.
	Kind Kind
	// Message says what went wrong.
	Message string
	// Fix says what to do about it. It is empty when there is nothing to do.
	Fix string
	// Errors lists every refused item of a batch (code invalid_batch only).
	Errors []BatchItemError
	// RetryAfter is how long the API asked us to wait (Retry-After), when it sent one.
	RetryAfter time.Duration

	cause error
}

// Error implements the error interface.
func (e *Error) Error() string {
	s := fmt.Sprintf("runearc: %s", e.Message)
	if e.Code != "" {
		s += fmt.Sprintf(" (%s", e.Code)
		if e.Status != 0 {
			s += fmt.Sprintf(", HTTP %d", e.Status)
		}
		s += ")"
	}
	if e.Fix != "" {
		s += ": " + e.Fix
	}
	return s
}

// Unwrap returns the underlying cause, such as a network error or
// [context.Canceled], so [errors.Is] sees through an *Error.
func (e *Error) Unwrap() error { return e.cause }

func kindFor(status int) Kind {
	switch {
	case status == http.StatusUnauthorized:
		return KindUnauthorized
	case status == http.StatusForbidden:
		return KindForbidden
	case status == http.StatusNotFound:
		return KindNotFound
	case status == http.StatusConflict:
		return KindConflict
	case status == http.StatusPaymentRequired:
		return KindLimitReached
	case status == http.StatusTooManyRequests:
		return KindRateLimited
	case status == http.StatusServiceUnavailable:
		return KindUnavailable
	case status >= 500:
		return KindInternal
	default:
		return KindInvalid
	}
}
