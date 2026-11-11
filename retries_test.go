package runearc_test

// Retry behaviour. A tiny local HTTP server stands in for failures the real API
// cannot be made to produce on demand. The idempotency test puts that server in
// front of the REAL API as a proxy: the first send reaches the API and sends,
// then the proxy answers 503 as if the response was lost; the SDK's retry must
// reuse the same Idempotency-Key so the API returns the first email instead of
// sending a second one.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	runearc "github.com/runearc-labs/runearc-go"
)

const unavailable = `{"error":{"code":"unavailable","message":"try again","fix":null}}`

func writeJSON(w http.ResponseWriter, status int, body string, headers ...string) {
	w.Header().Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		w.Header().Set(headers[i], headers[i+1])
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

type counter struct {
	mu    sync.Mutex
	times []time.Time
}

func (c *counter) hit() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.times = append(c.times, time.Now())
	return len(c.times)
}

func (c *counter) n() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.times)
}

func fake(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func fakeClient(t *testing.T, url string, opts ...runearc.Option) *runearc.Client {
	t.Helper()
	c, err := runearc.New("mb_test", append([]runearc.Option{runearc.WithBaseURL(url)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRetriesGETOn503AndHonoursRetryAfter(t *testing.T) {
	var hits counter
	s := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if hits.hit() == 1 {
			writeJSON(w, 503, unavailable, "Retry-After", "1")
			return
		}
		writeJSON(w, 200, `{"data":[]}`)
	})
	out, err := fakeClient(t, s.URL).Inboxes.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 0 || hits.n() != 2 {
		t.Fatalf("out %+v after %d calls", out, hits.n())
	}
	if gap := hits.times[1].Sub(hits.times[0]); gap < 950*time.Millisecond {
		t.Fatalf("retried after %s, Retry-After asked for 1s", gap)
	}
}

func TestRetriesHonoursRetryAfterHTTPDate(t *testing.T) {
	var hits counter
	s := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if hits.hit() == 1 {
			writeJSON(w, 429, `{"error":{"code":"rate_limited","message":"slow down","fix":null}}`,
				"Retry-After", time.Now().Add(2*time.Second).UTC().Format(http.TimeFormat))
			return
		}
		writeJSON(w, 200, `{"data":[]}`)
	})
	if _, err := fakeClient(t, s.URL).Inboxes.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gap := hits.times[1].Sub(hits.times[0]); gap < 900*time.Millisecond {
		t.Fatalf("retried after %s, Retry-After asked for about 2s", gap)
	}
}

func TestRetries429EvenOnPOSTWithoutKeyAndGivesUp(t *testing.T) {
	var hits counter
	s := fake(t, func(w http.ResponseWriter, r *http.Request) {
		hits.hit()
		writeJSON(w, 429, `{"error":{"code":"rate_limited","message":"slow down","fix":"wait"}}`, "Retry-After", "0")
	})
	_, err := fakeClient(t, s.URL, runearc.WithMaxRetries(3)).Inboxes.Create(context.Background(), runearc.CreateInboxRequest{})
	e := apiError(t, err)
	if hits.n() != 4 {
		t.Fatalf("%d calls, want 4", hits.n())
	}
	if e.Status != 429 || e.Code != "rate_limited" || e.Kind != runearc.KindRateLimited || e.Fix != "wait" || e.RetryAfter != 0 {
		t.Fatalf("got %+v", e)
	}
}

func TestRetryAfterIsOnTheError(t *testing.T) {
	s := fake(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 429, `{"error":{"code":"rate_limited","message":"slow down","fix":null}}`, "Retry-After", "7")
	})
	_, err := fakeClient(t, s.URL, runearc.WithMaxRetries(0)).Inboxes.List(context.Background())
	if e := apiError(t, err); e.RetryAfter != 7*time.Second {
		t.Fatalf("RetryAfter %s", e.RetryAfter)
	}
}

func TestNeverRetriesOther4xx(t *testing.T) {
	var hits counter
	s := fake(t, func(w http.ResponseWriter, r *http.Request) {
		hits.hit()
		writeJSON(w, 400, `{"error":{"code":"invalid_body","message":"bad","fix":"fix it"}}`)
	})
	_, err := fakeClient(t, s.URL).Emails.Send(context.Background(), &runearc.SendEmailRequest{From: "a@b.c", To: []string{"d@e.f"}, Subject: "s"})
	e := apiError(t, err)
	if hits.n() != 1 || e.Code != "invalid_body" || e.Fix != "fix it" || e.Kind != runearc.KindInvalid {
		t.Fatalf("%d calls, %+v", hits.n(), e)
	}
}

func TestDoesNotRepeatAPOSTWithoutKeyOn5xx(t *testing.T) {
	var hits counter
	s := fake(t, func(w http.ResponseWriter, r *http.Request) {
		hits.hit()
		writeJSON(w, 503, unavailable)
	})
	_, err := fakeClient(t, s.URL).Inboxes.Create(context.Background(), runearc.CreateInboxRequest{LocalPart: "x"})
	e := apiError(t, err)
	if hits.n() != 1 || e.Status != 503 || e.Kind != runearc.KindUnavailable {
		t.Fatalf("%d calls, %+v", hits.n(), e)
	}
}

func TestRetriesNetworkErrorsAndReportsThemTyped(t *testing.T) {
	var hits counter
	s := fake(t, func(w http.ResponseWriter, r *http.Request) {
		hits.hit()
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	})
	_, err := fakeClient(t, s.URL, runearc.WithMaxRetries(1)).Domains.List(context.Background())
	e := apiError(t, err)
	if hits.n() != 2 || e.Status != 0 || e.Code != "network_error" || e.Kind != runearc.KindNetwork {
		t.Fatalf("%d calls, %+v", hits.n(), e)
	}
	if e.Unwrap() == nil {
		t.Fatal("the cause is lost")
	}
}

func TestTimesOutPerAttempt(t *testing.T) {
	s := fake(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // never answers
	})
	c := fakeClient(t, s.URL, runearc.WithMaxRetries(0), runearc.WithTimeout(200*time.Millisecond))
	_, err := c.Events.List(context.Background(), runearc.EventsParams{})
	if e := apiError(t, err); e.Code != "timeout" || e.Kind != runearc.KindTimeout {
		t.Fatalf("got %+v", e)
	}
	_, err = fakeClient(t, s.URL, runearc.WithMaxRetries(0)).Events.List(context.Background(), runearc.EventsParams{}, runearc.WithRequestTimeout(200*time.Millisecond))
	if e := apiError(t, err); e.Code != "timeout" {
		t.Fatalf("per request timeout: %+v", e)
	}
}

func TestContextDeadlineEndsRetries(t *testing.T) {
	s := fake(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 503, unavailable, "Retry-After", "30")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := fakeClient(t, s.URL).Inboxes.List(ctx)
	e := apiError(t, err)
	if e.Code != "aborted" || !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Fatalf("got %+v after %s", e, time.Since(start))
	}
}

func TestRetriedSendReusesItsIdempotencyKey(t *testing.T) {
	var hits counter
	var mu sync.Mutex
	var keys []string
	proxy := fake(t, func(w http.ResponseWriter, r *http.Request) {
		n := hits.hit()
		mu.Lock()
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		req, err := http.NewRequestWithContext(r.Context(), r.Method, stack.apiURL+r.URL.RequestURI(), strings.NewReader(string(body)))
		if err != nil {
			t.Error(err)
			return
		}
		req.Header.Set("Authorization", r.Header.Get("Authorization"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", r.Header.Get("Idempotency-Key"))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error(err)
			return
		}
		defer res.Body.Close()
		text, _ := io.ReadAll(res.Body)
		// The first answer is "lost": the email was sent, but the client sees a 503.
		if n == 1 {
			writeJSON(w, 503, unavailable)
			return
		}
		writeJSON(w, res.StatusCode, string(text))
	})
	proxied, err := runearc.New(stack.apiKey, runearc.WithBaseURL(proxy.URL))
	if err != nil {
		t.Fatal(err)
	}
	subject := unique("retry once")
	sent, err := proxied.Emails.Send(context.Background(), &runearc.SendEmailRequest{From: from(), To: []string{testAddress("delivered")}, Subject: subject, Text: "once"})
	if err != nil {
		t.Fatal(err)
	}
	if hits.n() != 2 {
		t.Fatalf("%d calls, want 2", hits.n())
	}
	if !regexp.MustCompile(`^[0-9a-f-]{36}$`).MatchString(keys[0]) || keys[1] != keys[0] {
		t.Fatalf("keys %v", keys)
	}
	page, err := client(t).Emails.List(context.Background(), runearc.PageParams{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range page.Data {
		if e.Subject == subject {
			ids = append(ids, e.ID)
		}
	}
	if len(ids) != 1 || ids[0] != sent.ID {
		t.Fatalf("emails with that subject: %v, want only %s", ids, sent.ID)
	}
}

func TestSendingCallsGetAnAutomaticKeyAndOthersDoNot(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]string{}
	s := fake(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.Method+" "+r.URL.Path] = r.Header.Get("Idempotency-Key")
		mu.Unlock()
		writeJSON(w, 200, `{}`)
	})
	c := fakeClient(t, s.URL)
	ctx := context.Background()
	_, _ = c.Emails.Send(ctx, &runearc.SendEmailRequest{})
	_, _ = c.Emails.Batch(ctx, nil)
	_, _ = c.Messages.Send(ctx, "ibx_1", &runearc.SendMessageRequest{})
	_, _ = c.Messages.Reply(ctx, "msg_1", &runearc.ReplyRequest{})
	_, _ = c.Inboxes.Create(ctx, runearc.CreateInboxRequest{})
	_, _ = c.Emails.Send(ctx, &runearc.SendEmailRequest{}, runearc.WithIdempotencyKey("mine"))
	mu.Lock()
	defer mu.Unlock()
	for _, k := range []string{"POST /v1/emails/batch", "POST /v1/inboxes/ibx_1/messages", "POST /v1/messages/msg_1/reply"} {
		if seen[k] == "" {
			t.Errorf("%s has no automatic Idempotency-Key", k)
		}
	}
	if seen["POST /v1/inboxes"] != "" {
		t.Error("creating an inbox must not carry an Idempotency-Key")
	}
	if seen["POST /v1/emails"] != "mine" {
		t.Errorf("the given key was not sent: %q", seen["POST /v1/emails"])
	}
}

func TestRequestHeaders(t *testing.T) {
	var got http.Header
	s := fake(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		writeJSON(w, 200, `{"data":[]}`)
	})
	c := fakeClient(t, s.URL, runearc.WithHeader("Runearc-Project", "prj_1"), runearc.WithHeader("Authorization", "Bearer evil"))
	if _, err := c.Senders.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got.Get("Authorization") != "Bearer mb_test" || got.Get("Runearc-Project") != "prj_1" ||
		got.Get("Accept") != "application/json" || got.Get("User-Agent") != "runearc-go/"+runearc.Version {
		t.Fatalf("headers %v", got)
	}
}

func TestBatchValidationHeader(t *testing.T) {
	var got string
	s := fake(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("x-batch-validation")
		writeJSON(w, 200, `{"data":[]}`)
	})
	if _, err := fakeClient(t, s.URL).Emails.Batch(context.Background(), nil, runearc.WithBatchValidation(runearc.BatchPermissive)); err != nil {
		t.Fatal(err)
	}
	if got != "permissive" {
		t.Fatalf("header %q", got)
	}
}

func TestInvalidResponseAndPlainErrors(t *testing.T) {
	s := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/domains" {
			io.WriteString(w, "<html>not the API</html>")
			return
		}
		http.Error(w, "gateway on fire", http.StatusBadGateway)
	})
	c := fakeClient(t, s.URL, runearc.WithMaxRetries(0))
	_, err := c.Domains.List(context.Background())
	if e := apiError(t, err); e.Code != "invalid_response" || e.Kind != runearc.KindClient || e.Status != 200 {
		t.Fatalf("got %+v", e)
	}
	_, err = c.Keys.List(context.Background())
	if e := apiError(t, err); e.Code != "http_502" || e.Status != 502 || !strings.Contains(e.Message, "gateway on fire") || e.Kind != runearc.KindInternal {
		t.Fatalf("got %+v", e)
	}
}

func TestEnvironmentAndBaseURL(t *testing.T) {
	t.Setenv("RUNEARC_API_KEY", "")
	t.Setenv("RUNEARC_BASE_URL", "")

	_, err := runearc.New("")
	e := apiError(t, err)
	if e.Code != "missing_api_key" || e.Fix == "" || e.Kind != runearc.KindClient {
		t.Fatalf("got %+v", e)
	}

	t.Setenv("RUNEARC_API_KEY", "mb_from_env")
	c, err := runearc.New("")
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL() != "https://api.runearc.com" || runearc.DefaultBaseURL != "https://api.runearc.com" {
		t.Fatalf("base URL %q", c.BaseURL())
	}

	t.Setenv("RUNEARC_BASE_URL", "http://127.0.0.1:1/v1/")
	c, err = runearc.New("")
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL() != "http://127.0.0.1:1" {
		t.Fatalf("base URL %q", c.BaseURL())
	}

	// An option beats the environment, and /v1 is tolerated there too.
	c, err = runearc.New("mb_key", runearc.WithBaseURL("http://localhost:9/v1"))
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL() != "http://localhost:9" {
		t.Fatalf("base URL %q", c.BaseURL())
	}
}

func TestBaseURLWithV1ReachesTheRealAPI(t *testing.T) {
	c := clientWithKey(t, stack.apiKey, runearc.WithBaseURL(stack.apiURL+"/v1/"))
	if _, err := c.Senders.List(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCustomHTTPClient(t *testing.T) {
	var used bool
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		used = true
		return http.DefaultTransport.RoundTrip(r)
	})
	c := client(t, runearc.WithHTTPClient(&http.Client{Transport: rt}))
	if _, err := c.Senders.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !used {
		t.Fatal("the custom client was not used")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
