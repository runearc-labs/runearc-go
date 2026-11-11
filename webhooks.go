package runearc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// WebhookTolerance is how far a webhook timestamp may be from now before it is
// refused as a possible replay.
const WebhookTolerance = 5 * time.Minute

// WebhooksService manages webhook endpoints and verifies deliveries.
type WebhooksService struct{ c *Client }

// Create creates an endpoint. The response has the signing Secret, shown once.
func (s *WebhooksService) Create(ctx context.Context, req CreateWebhookRequest, opts ...RequestOption) (*CreatedWebhook, error) {
	var out CreatedWebhook
	err := s.c.doJSON(ctx, call{method: http.MethodPost, path: "/v1/webhooks", body: req}, opts, &out)
	return orNil(&out, err)
}

// List returns every endpoint.
func (s *WebhooksService) List(ctx context.Context, opts ...RequestOption) (*List[Webhook], error) {
	var out List[Webhook]
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/webhooks"}, opts, &out)
	return orNil(&out, err)
}

// Delete deletes an endpoint.
func (s *WebhooksService) Delete(ctx context.Context, id string, opts ...RequestOption) error {
	return s.c.doJSON(ctx, call{method: http.MethodDelete, path: "/v1/webhooks/" + seg(id)}, opts, nil)
}

// Deliveries returns the latest deliveries of an endpoint. A limit of 0 uses the API default.
func (s *WebhooksService) Deliveries(ctx context.Context, id string, limit int, opts ...RequestOption) (*List[Delivery], error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out List[Delivery]
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/webhooks/" + seg(id) + "/deliveries", query: q}, opts, &out)
	return orNil(&out, err)
}

// Replay delivers the event of a past delivery again.
func (s *WebhooksService) Replay(ctx context.Context, id, deliveryID string, opts ...RequestOption) (*Delivery, error) {
	var out Delivery
	err := s.c.doJSON(ctx, call{method: http.MethodPost, path: "/v1/webhooks/" + seg(id) + "/deliveries/" + seg(deliveryID) + "/replay"}, opts, &out)
	return orNil(&out, err)
}

// Verify is the same as the package level [VerifyWebhook].
func (s *WebhooksService) Verify(in VerifyWebhookInput) (*WebhookEvent, error) {
	return VerifyWebhook(in)
}

// VerifyWebhookInput is what [VerifyWebhook] checks.
type VerifyWebhookInput struct {
	// Payload is the raw request body, exactly as received. Do not parse it and
	// encode it again.
	Payload []byte
	// Headers are the request headers. Names are matched without regard to case.
	Headers http.Header
	// Secret is the endpoint's signing secret, whsec_..., from [WebhooksService.Create].
	Secret string
	// Tolerance is the clock difference allowed. The default is [WebhookTolerance].
	Tolerance time.Duration
	// Now is the current time, for tests. The default is time.Now().
	Now time.Time
}

func headerValue(h http.Header, name string) string {
	var vals []string
	for k, v := range h {
		if strings.EqualFold(k, name) {
			vals = append(vals, v...)
		}
	}
	return strings.Join(vals, " ")
}

func webhookError(code, message, fix string) *Error {
	return &Error{Code: code, Kind: KindWebhook, Message: message, Fix: fix}
}

// VerifyWebhook checks that a webhook request came from Runearc and is fresh,
// then returns the parsed event. Runearc signs deliveries with Standard Webhooks:
// the headers webhook-id, webhook-timestamp and webhook-signature
// ("v1,<base64 HMAC SHA256 of id.timestamp.body>", several separated by spaces
// during key rotation). Failures are an *[Error] with Kind [KindWebhook] and one
// of the codes missing_headers, invalid_timestamp, timestamp_out_of_range,
// invalid_secret, invalid_signature or invalid_payload.
func VerifyWebhook(in VerifyWebhookInput) (*WebhookEvent, error) {
	id := headerValue(in.Headers, "webhook-id")
	ts := headerValue(in.Headers, "webhook-timestamp")
	sigs := headerValue(in.Headers, "webhook-signature")
	if id == "" || ts == "" || sigs == "" {
		return nil, webhookError("missing_headers",
			"the webhook-id, webhook-timestamp and webhook-signature headers are required",
			"Pass the request headers unchanged; check that a proxy does not strip them")
	}
	ts = strings.TrimSpace(ts)
	seconds, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || seconds < 0 || strings.HasPrefix(ts, "+") {
		return nil, webhookError("invalid_timestamp", "webhook-timestamp is not a number of seconds", "")
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	tolerance := in.Tolerance
	if tolerance <= 0 {
		tolerance = WebhookTolerance
	}
	diff := now.Sub(time.Unix(seconds, 0))
	if diff < 0 {
		diff = -diff
	}
	if diff > tolerance {
		return nil, webhookError("timestamp_out_of_range",
			"the webhook timestamp is more than "+tolerance.String()+" from now",
			"Check your server clock; old deliveries are refused to stop replays")
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(strings.TrimSpace(in.Secret), "whsec_"))
	if err != nil {
		return nil, webhookError("invalid_secret", "the webhook secret is not valid base64",
			"Use the secret returned by Webhooks.Create, which starts with whsec_")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(in.Payload)
	want := mac.Sum(nil)
	for _, part := range strings.Fields(sigs) {
		version, sig, ok := strings.Cut(part, ",")
		if !ok || version != "v1" || sig == "" {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(sig)
		if err != nil {
			continue
		}
		if hmac.Equal(got, want) {
			var ev WebhookEvent
			if err := json.Unmarshal(in.Payload, &ev); err != nil {
				return nil, webhookError("invalid_payload", "the webhook body is not a JSON event", "")
			}
			return &ev, nil
		}
	}
	return nil, webhookError("invalid_signature", "no webhook-signature matches the payload",
		"Verify the raw body (not JSON parsed and encoded again) with this endpoint's whsec_ secret")
}
