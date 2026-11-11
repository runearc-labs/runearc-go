package runearc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// Version is the SDK version, sent in the User-Agent header.
	Version = "0.1.0"
	// DefaultBaseURL is the API origin used when none is configured.
	DefaultBaseURL = "https://api.runearc.com"

	defaultMaxRetries = 2
	defaultTimeout    = 60 * time.Second
	maxRetryWait      = 60 * time.Second
)

// Client talks to the Runearc API. It is safe for concurrent use. Create one
// with [New].
type Client struct {
	// Emails sends, schedules and reads email.
	Emails *EmailsService
	// Senders lists the addresses you can send from.
	Senders *SendersService
	// Inboxes creates and manages inboxes.
	Inboxes *InboxesService
	// Messages reads, waits for, sends and replies to inbox messages.
	Messages *MessagesService
	// Threads reads the threads of an inbox.
	Threads *ThreadsService
	// Conversations is the shared support view across inboxes.
	Conversations *ConversationsService
	// Domains manages sending and receiving domains.
	Domains *DomainsService
	// Webhooks manages webhook endpoints and verifies deliveries.
	Webhooks *WebhooksService
	// Keys manages API keys.
	Keys *KeysService
	// Suppressions lists and removes suppressed addresses.
	Suppressions *SuppressionsService
	// Events reads the event log.
	Events *EventsService

	apiKey     string
	baseURL    string
	maxRetries int
	timeout    time.Duration
	http       *http.Client
	headers    map[string]string
}

// Option configures a [Client].
type Option func(*Client)

// WithBaseURL sets the API origin. The default is the RUNEARC_BASE_URL
// environment variable, or [DefaultBaseURL]. A trailing /v1 is fine.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithMaxRetries sets the retries after the first attempt, on 429, 5xx and
// network errors. The default is 2; a negative value means 0.
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// WithTimeout sets the timeout of each attempt. The default is 60 seconds.
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.timeout = d } }

// WithHTTPClient sets the [http.Client] to send requests with. Per attempt
// timeouts are applied through the request context, not the client's Timeout.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithHeader adds a header to every request, such as Runearc-Project. The
// Authorization header cannot be replaced.
func WithHeader(name, value string) Option {
	return func(c *Client) { c.headers[name] = value }
}

// New creates a client. An empty apiKey is read from the RUNEARC_API_KEY
// environment variable; if that is empty too, New returns an *[Error] with
// code missing_api_key.
func New(apiKey string, opts ...Option) (*Client, error) {
	c := &Client{
		maxRetries: defaultMaxRetries,
		timeout:    defaultTimeout,
		headers:    map[string]string{},
	}
	for _, o := range opts {
		o(c)
	}
	if apiKey == "" {
		apiKey = os.Getenv("RUNEARC_API_KEY")
	}
	if apiKey == "" {
		return nil, &Error{
			Code:    "missing_api_key",
			Kind:    KindClient,
			Message: "no Runearc API key was given",
			Fix:     "Pass it to runearc.New or set RUNEARC_API_KEY",
		}
	}
	c.apiKey = apiKey
	if c.baseURL == "" {
		c.baseURL = os.Getenv("RUNEARC_BASE_URL")
	}
	if c.baseURL == "" {
		c.baseURL = DefaultBaseURL
	}
	c.baseURL = strings.TrimSuffix(strings.TrimRight(c.baseURL, "/"), "/v1")
	if c.maxRetries < 0 {
		c.maxRetries = 0
	}
	if c.http == nil {
		c.http = &http.Client{}
	}
	c.Emails = &EmailsService{c}
	c.Senders = &SendersService{c}
	c.Inboxes = &InboxesService{c}
	c.Messages = &MessagesService{c}
	c.Threads = &ThreadsService{c}
	c.Conversations = &ConversationsService{c}
	c.Domains = &DomainsService{c}
	c.Webhooks = &WebhooksService{c}
	c.Keys = &KeysService{c}
	c.Suppressions = &SuppressionsService{c}
	c.Events = &EventsService{c}
	return c, nil
}

// BaseURL returns the API origin the client uses, without a trailing /v1.
func (c *Client) BaseURL() string { return c.baseURL }

// BatchValidation is how [EmailsService.Batch] treats invalid items.
type BatchValidation string

const (
	// BatchStrict refuses the whole batch with invalid_batch when one item is
	// invalid. It is the default.
	BatchStrict BatchValidation = "strict"
	// BatchPermissive sends the valid items and reports the invalid ones in
	// [BatchResponse].Errors.
	BatchPermissive BatchValidation = "permissive"
)

type requestConfig struct {
	idempotencyKey  string
	timeout         time.Duration
	batchValidation BatchValidation
}

// RequestOption changes a single call.
type RequestOption func(*requestConfig)

// WithIdempotencyKey makes the request safe to repeat: the API returns the first
// result for the same key for 24 hours. Calls that send mail get a random key
// when you give none.
func WithIdempotencyKey(key string) RequestOption {
	return func(r *requestConfig) { r.idempotencyKey = key }
}

// WithRequestTimeout overrides the client's per attempt timeout for one call.
func WithRequestTimeout(d time.Duration) RequestOption {
	return func(r *requestConfig) { r.timeout = d }
}

// WithBatchValidation sets how [EmailsService.Batch] treats invalid items.
// Other calls ignore it.
func WithBatchValidation(v BatchValidation) RequestOption {
	return func(r *requestConfig) { r.batchValidation = v }
}

type call struct {
	method string
	path   string
	query  url.Values
	body   any
	header map[string]string
	// sendsMail marks a call that gets an automatic Idempotency-Key.
	sendsMail bool
	// idempotent marks a POST that is safe to repeat (verify, cancel, mark read).
	idempotent bool
	// timeout is the default per attempt timeout of this call, when not the client's.
	timeout time.Duration
}

func seg(s string) string { return url.PathEscape(s) }

// doJSON runs a call and decodes a JSON response into out (nil to ignore it).
func (c *Client) doJSON(ctx context.Context, cl call, opts []RequestOption, out any) error {
	raw, status, err := c.send(ctx, cl, opts)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &Error{
			Status:  status,
			Code:    "invalid_response",
			Kind:    KindClient,
			Message: "the API sent a response that is not the expected JSON",
			Fix:     "Check that the base URL points at the Runearc API",
			cause:   err,
		}
	}
	return nil
}

// send runs a call with retries and returns the body of the successful response.
func (c *Client) send(ctx context.Context, cl call, opts []RequestOption) ([]byte, int, error) {
	var rc requestConfig
	for _, o := range opts {
		o(&rc)
	}
	u := c.baseURL + cl.path
	if len(cl.query) > 0 {
		u += "?" + cl.query.Encode()
	}
	header := http.Header{}
	header.Set("Accept", "application/json")
	header.Set("User-Agent", "runearc-go/"+Version)
	for k, v := range c.headers {
		header.Set(k, v)
	}
	for k, v := range cl.header {
		header.Set(k, v)
	}
	if rc.batchValidation != "" {
		header.Set("x-batch-validation", string(rc.batchValidation))
	}
	header.Set("Authorization", "Bearer "+c.apiKey)
	// One key for every attempt, so a retry returns the first result instead of sending again.
	idemKey := rc.idempotencyKey
	if idemKey == "" && cl.sendsMail {
		k, err := randomID()
		if err != nil {
			return nil, 0, &Error{Code: "random_unavailable", Kind: KindClient, Message: "could not generate an idempotency key", cause: err}
		}
		idemKey = k
	}
	if idemKey != "" {
		header.Set("Idempotency-Key", idemKey)
	}
	var payload []byte
	if cl.body != nil {
		b, err := json.Marshal(cl.body)
		if err != nil {
			return nil, 0, &Error{Code: "invalid_request", Kind: KindClient, Message: "the request body cannot be encoded as JSON", cause: err}
		}
		payload = b
		header.Set("Content-Type", "application/json")
	}
	timeout := c.timeout
	if cl.timeout > 0 {
		timeout = cl.timeout
	}
	if rc.timeout > 0 {
		timeout = rc.timeout
	}
	// Repeating must not create a second resource: GET, DELETE and PATCH are
	// idempotent, and a POST is when it carries an Idempotency-Key.
	repeatable := cl.method != http.MethodPost || idemKey != "" || cl.idempotent

	for attempt := 0; ; attempt++ {
		last := attempt >= c.maxRetries
		res := c.attempt(ctx, cl.method, u, header, payload, timeout)
		if res.err != nil {
			if ctx.Err() != nil {
				return nil, 0, &Error{Code: "aborted", Kind: KindCanceled, Message: "the request was canceled", cause: ctx.Err()}
			}
			if repeatable && !last {
				if err := sleep(ctx, c.backoff(attempt)); err != nil {
					return nil, 0, err
				}
				continue
			}
			return nil, 0, c.transportError(res, idemKey, timeout)
		}
		if res.status >= 200 && res.status < 300 {
			return res.body, res.status, nil
		}
		wait, hasWait := retryAfter(res.header.Get("Retry-After"))
		retryable := res.status == http.StatusTooManyRequests || (res.status >= 500 && repeatable)
		if retryable && !last {
			d := c.backoff(attempt)
			if hasWait {
				d = wait
			}
			if d > maxRetryWait {
				d = maxRetryWait
			}
			if err := sleep(ctx, d); err != nil {
				return nil, 0, err
			}
			continue
		}
		e := apiError(res.status, res.body)
		if hasWait {
			e.RetryAfter = wait
		}
		return nil, res.status, e
	}
}

type attemptResult struct {
	status   int
	header   http.Header
	body     []byte
	err      error
	timedOut bool
}

func (c *Client) attempt(ctx context.Context, method, u string, header http.Header, payload []byte, timeout time.Duration) attemptResult {
	actx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(actx, method, u, body)
	if err != nil {
		return attemptResult{err: err}
	}
	req.Header = header.Clone()
	resp, err := c.http.Do(req)
	if err != nil {
		return attemptResult{err: err, timedOut: isDeadline(actx, ctx)}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return attemptResult{err: err, timedOut: isDeadline(actx, ctx)}
	}
	return attemptResult{status: resp.StatusCode, header: resp.Header, body: data}
}

// isDeadline reports whether the attempt's own timeout fired, not the caller's context.
func isDeadline(attempt, parent context.Context) bool {
	return parent.Err() == nil && errors.Is(attempt.Err(), context.DeadlineExceeded)
}

func (c *Client) transportError(res attemptResult, idemKey string, timeout time.Duration) *Error {
	e := &Error{cause: res.err}
	if res.timedOut {
		e.Code, e.Kind = "timeout", KindTimeout
		e.Message = fmt.Sprintf("no response from %s within %s", c.baseURL, timeout)
	} else {
		e.Code, e.Kind = "network_error", KindNetwork
		e.Message = fmt.Sprintf("could not reach %s: %v", c.baseURL, res.err)
	}
	if idemKey != "" {
		e.Fix = fmt.Sprintf("Retry with the same idempotency key (%s) so the email is not sent twice", idemKey)
	} else {
		e.Fix = "Check your network and base URL, then retry"
	}
	return e
}

// backoff is exponential with jitter: half of the cap plus a random part of the
// other half, the cap doubling from 500 ms up to 8 s.
func (c *Client) backoff(attempt int) time.Duration {
	capMs := math.Min(8000, 500*math.Pow(2, float64(attempt)))
	jitter := 0.0
	if n, err := rand.Int(rand.Reader, big.NewInt(1000)); err == nil {
		jitter = float64(n.Int64()) / 1000
	}
	return time.Duration((capMs/2 + jitter*capMs/2) * float64(time.Millisecond))
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return &Error{Code: "aborted", Kind: KindCanceled, Message: "the request was canceled", cause: ctx.Err()}
	case <-t.C:
		return nil
	}
}

// retryAfter parses a Retry-After header: seconds or an HTTP date.
func retryAfter(v string) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil && !math.IsNaN(secs) && !math.IsInf(secs, 0) {
		return time.Duration(math.Max(0, secs) * float64(time.Second)), true
	}
	if at, err := http.ParseTime(v); err == nil {
		return max(0, time.Until(at)), true
	}
	return 0, false
}

func apiError(status int, body []byte) *Error {
	var env struct {
		Error *struct {
			Code    string           `json:"code"`
			Message string           `json:"message"`
			Fix     *string          `json:"fix"`
			Errors  []BatchItemError `json:"errors"`
		} `json:"error"`
	}
	e := &Error{Status: status, Kind: kindFor(status)}
	if json.Unmarshal(body, &env) == nil && env.Error != nil {
		e.Code = env.Error.Code
		e.Message = env.Error.Message
		if env.Error.Fix != nil {
			e.Fix = *env.Error.Fix
		}
		e.Errors = env.Error.Errors
	}
	if e.Code == "" {
		e.Code = fmt.Sprintf("http_%d", status)
	}
	if e.Message == "" {
		text := strings.TrimSpace(string(body))
		if len(text) > 500 {
			text = text[:500]
		}
		if text == "" {
			text = http.StatusText(status)
		}
		if text == "" {
			text = fmt.Sprintf("HTTP %d", status)
		}
		e.Message = text
	}
	return e
}

// randomID returns a random version 4 UUID.
func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
