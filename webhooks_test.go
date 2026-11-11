package runearc_test

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	runearc "github.com/runearc-labs/runearc-go"
)

// sign computes a signature independently of the SDK, the way
// runearc_core::webhooks::sign does it.
func sign(t *testing.T, secret, id string, ts int64, body string) string {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil {
		t.Fatal(err)
	}
	m := hmac.New(sha256.New, key)
	fmt.Fprintf(m, "%s.%d.%s", id, ts, body)
	return "v1," + base64.StdEncoding.EncodeToString(m.Sum(nil))
}

func newSecret(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return "whsec_" + base64.StdEncoding.EncodeToString(b)
}

func webhookCode(t *testing.T, err error) string {
	t.Helper()
	e := apiError(t, err)
	if e.Kind != runearc.KindWebhook || e.Status != 0 {
		t.Fatalf("got %+v", e)
	}
	return e.Code
}

func TestVerifyWebhook(t *testing.T) {
	secret := newSecret(t)
	body := `{"id":"evt_1","type":"message.delivered","timestamp":"2026-10-14T10:00:00.000Z","data":{"email_id":"eml_1"}}`
	now := time.Now()
	ts := now.Unix()
	headers := func() http.Header {
		return http.Header{
			"Webhook-Id":        {"evt_1"},
			"Webhook-Timestamp": {fmt.Sprint(ts)},
			"Webhook-Signature": {sign(t, secret, "evt_1", ts, body)},
		}
	}

	t.Run("matches the Standard Webhooks test vector (same as the Rust unit test)", func(t *testing.T) {
		ev, err := runearc.VerifyWebhook(runearc.VerifyWebhookInput{
			Payload: []byte(`{"test": 2432232314}`),
			Secret:  "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			Headers: http.Header{
				"Webhook-Id":        {"msg_p5jXN8AQM9LWM0D4loKWxJek"},
				"Webhook-Timestamp": {"1614265330"},
				"Webhook-Signature": {"v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE="},
			},
			Now: time.Unix(1614265330, 0),
		})
		if err != nil {
			t.Fatal(err)
		}
		if ev == nil {
			t.Fatal("no event")
		}
	})

	t.Run("accepts a valid signature, any header case, and several signatures", func(t *testing.T) {
		ev, err := runearc.VerifyWebhook(runearc.VerifyWebhookInput{Payload: []byte(body), Headers: headers(), Secret: secret})
		if err != nil {
			t.Fatal(err)
		}
		if ev.ID != "evt_1" || ev.Type != "message.delivered" || ev.Timestamp.Year() != 2026 || !strings.Contains(string(ev.Data), "eml_1") {
			t.Fatalf("event %+v", ev)
		}
		upper := http.Header{}
		for k, v := range headers() {
			upper[strings.ToUpper(k)] = v
		}
		if _, err := runearc.VerifyWebhook(runearc.VerifyWebhookInput{Payload: []byte(body), Headers: upper, Secret: secret}); err != nil {
			t.Fatal(err)
		}
		multi := headers()
		multi.Set("Webhook-Signature", "v1,AAAA "+multi.Get("Webhook-Signature"))
		if _, err := runearc.VerifyWebhook(runearc.VerifyWebhookInput{Payload: []byte(body), Headers: multi, Secret: secret}); err != nil {
			t.Fatal(err)
		}
		c := client(t)
		if _, err := c.Webhooks.Verify(runearc.VerifyWebhookInput{Payload: []byte(body), Headers: headers(), Secret: secret}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("refuses tampered bodies and wrong secrets", func(t *testing.T) {
		_, err := runearc.VerifyWebhook(runearc.VerifyWebhookInput{Payload: []byte(strings.Replace(body, "eml_1", "eml_2", 1)), Headers: headers(), Secret: secret})
		if code := webhookCode(t, err); code != "invalid_signature" {
			t.Fatal(code)
		}
		_, err = runearc.VerifyWebhook(runearc.VerifyWebhookInput{Payload: []byte(body), Headers: headers(), Secret: newSecret(t)})
		if code := webhookCode(t, err); code != "invalid_signature" {
			t.Fatal(code)
		}
		_, err = runearc.VerifyWebhook(runearc.VerifyWebhookInput{Payload: []byte(body), Headers: headers(), Secret: "whsec_not base64!"})
		if code := webhookCode(t, err); code != "invalid_secret" {
			t.Fatal(code)
		}
	})

	t.Run("refuses stale timestamps, bad timestamps and missing headers", func(t *testing.T) {
		old := now.Unix() - 301
		stale := http.Header{
			"Webhook-Id":        {"evt_1"},
			"Webhook-Timestamp": {fmt.Sprint(old)},
			"Webhook-Signature": {sign(t, secret, "evt_1", old, body)},
		}
		_, err := runearc.VerifyWebhook(runearc.VerifyWebhookInput{Payload: []byte(body), Headers: stale, Secret: secret})
		if code := webhookCode(t, err); code != "timestamp_out_of_range" {
			t.Fatal(code)
		}
		// A wider tolerance accepts it.
		if _, err := runearc.VerifyWebhook(runearc.VerifyWebhookInput{Payload: []byte(body), Headers: stale, Secret: secret, Tolerance: time.Hour}); err != nil {
			t.Fatal(err)
		}
		future := now.Unix() + 301
		ahead := http.Header{
			"Webhook-Id":        {"evt_1"},
			"Webhook-Timestamp": {fmt.Sprint(future)},
			"Webhook-Signature": {sign(t, secret, "evt_1", future, body)},
		}
		_, err = runearc.VerifyWebhook(runearc.VerifyWebhookInput{Payload: []byte(body), Headers: ahead, Secret: secret})
		if code := webhookCode(t, err); code != "timestamp_out_of_range" {
			t.Fatal(code)
		}
		bad := headers()
		bad.Set("Webhook-Timestamp", "soon")
		_, err = runearc.VerifyWebhook(runearc.VerifyWebhookInput{Payload: []byte(body), Headers: bad, Secret: secret})
		if code := webhookCode(t, err); code != "invalid_timestamp" {
			t.Fatal(code)
		}
		_, err = runearc.VerifyWebhook(runearc.VerifyWebhookInput{Payload: []byte(body), Headers: http.Header{"Webhook-Id": {"evt_1"}}, Secret: secret})
		if code := webhookCode(t, err); code != "missing_headers" {
			t.Fatal(code)
		}
	})

	t.Run("refuses a signed body that is not JSON", func(t *testing.T) {
		notJSON := "plain text"
		h := http.Header{
			"Webhook-Id":        {"evt_1"},
			"Webhook-Timestamp": {fmt.Sprint(ts)},
			"Webhook-Signature": {sign(t, secret, "evt_1", ts, notJSON)},
		}
		_, err := runearc.VerifyWebhook(runearc.VerifyWebhookInput{Payload: []byte(notJSON), Headers: h, Secret: secret})
		if code := webhookCode(t, err); code != "invalid_payload" {
			t.Fatal(code)
		}
	})
}

type receivedHook struct {
	headers http.Header
	body    string
}

func TestWebhooksRealAPIAndWorker(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	var received []receivedHook
	snapshot := func() []receivedHook {
		mu.Lock()
		defer mu.Unlock()
		return append([]receivedHook(nil), received...)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, receivedHook{headers: r.Header.Clone(), body: string(b)})
		mu.Unlock()
		_, _ = io.WriteString(w, "ok")
	}))
	// The worker keeps its connection alive; drop it so Close does not wait for it.
	defer func() {
		server.CloseClientConnections()
		server.Close()
	}()

	c := client(t)
	hook, err := c.Webhooks.Create(ctx, runearc.CreateWebhookRequest{URL: server.URL + "/hook", EventTypes: []string{"message.delivered"}})
	if err != nil {
		t.Fatal(err)
	}
	if hook.ID == "" || !strings.HasPrefix(hook.Secret, "whsec_") {
		t.Fatalf("hook %+v", hook)
	}
	list, err := c.Webhooks.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFunc(list.Data, func(w runearc.Webhook) bool { return w.ID == hook.ID }) {
		t.Fatal("webhook is not in the list")
	}

	sent, err := c.Emails.Send(ctx, &runearc.SendEmailRequest{From: from(), To: []string{testAddress("delivered")}, Subject: unique("hook"), Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	var first receivedHook
	eventually(t, 30*time.Second, func() (bool, error) {
		for _, r := range snapshot() {
			if strings.Contains(r.body, sent.ID) {
				first = r
				return true, nil
			}
		}
		return false, nil
	})
	event, err := c.Webhooks.Verify(runearc.VerifyWebhookInput{Payload: []byte(first.body), Headers: first.headers, Secret: hook.Secret})
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "message.delivered" || event.ID != first.headers.Get("Webhook-Id") || !strings.Contains(string(event.Data), sent.ID) {
		t.Fatalf("event %+v", event)
	}

	var delivery runearc.Delivery
	eventually(t, 15*time.Second, func() (bool, error) {
		d, err := c.Webhooks.Deliveries(ctx, hook.ID, 10)
		if err != nil || len(d.Data) == 0 {
			return false, err
		}
		for _, x := range d.Data {
			if x.Status == "pending" {
				return false, nil
			}
		}
		delivery = d.Data[0]
		return true, nil
	})
	before := len(snapshot())
	replay, err := c.Webhooks.Replay(ctx, hook.ID, delivery.ID)
	if err != nil {
		t.Fatal(err)
	}
	if replay.ReplayOf == nil || *replay.ReplayOf != delivery.ID {
		t.Fatalf("replay %+v", replay)
	}
	var again receivedHook
	eventually(t, 30*time.Second, func() (bool, error) {
		all := snapshot()
		if len(all) > before {
			again = all[len(all)-1]
			return true, nil
		}
		return false, nil
	})
	replayed, err := runearc.VerifyWebhook(runearc.VerifyWebhookInput{Payload: []byte(again.body), Headers: again.headers, Secret: hook.Secret})
	if err != nil || replayed.Type != "message.delivered" {
		t.Fatalf("replayed: %v %+v", err, replayed)
	}

	if err := c.Webhooks.Delete(ctx, hook.ID); err != nil {
		t.Fatal(err)
	}
	list, err = c.Webhooks.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if containsFunc(list.Data, func(w runearc.Webhook) bool { return w.ID == hook.ID }) {
		t.Fatal("webhook was not deleted")
	}
}
