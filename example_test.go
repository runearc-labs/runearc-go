package runearc_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	runearc "github.com/runearc-labs/runearc-go"
)

func ExampleNew() {
	// An empty key reads RUNEARC_API_KEY.
	client, err := runearc.New("",
		runearc.WithMaxRetries(3),
		runearc.WithTimeout(30*time.Second),
		runearc.WithHeader("Runearc-Project", "prj_123"),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(client.BaseURL())
}

func ExampleEmailsService_Send() {
	ctx := context.Background()
	client, err := runearc.New("mb_live_xxxxxxxxx")
	if err != nil {
		log.Fatal(err)
	}
	sent, err := client.Emails.Send(ctx, &runearc.SendEmailRequest{
		From:    "Acme <noreply@acme.com>",
		To:      []string{"bob@example.com"},
		Subject: "Welcome to Acme",
		HTML:    "<p>Thanks for signing up.</p>",
		Tags:    []runearc.Tag{{Name: "kind", Value: "welcome"}},
	}, runearc.WithIdempotencyKey("welcome/user_42"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(sent.ID)
}

func ExampleEmailsService_Send_scheduled() {
	ctx := context.Background()
	client, err := runearc.New("mb_live_xxxxxxxxx")
	if err != nil {
		log.Fatal(err)
	}
	sent, err := client.Emails.Send(ctx, &runearc.SendEmailRequest{
		From:        "Acme <noreply@acme.com>",
		To:          []string{"bob@example.com"},
		Subject:     "Your trial ends tomorrow",
		Text:        "Here is what happens next.",
		ScheduledAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339), // or "in 1 day"
	})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := client.Emails.Reschedule(ctx, sent.ID, "in 2 days"); err != nil {
		log.Fatal(err)
	}
	if _, err := client.Emails.Cancel(ctx, sent.ID); err != nil {
		log.Fatal(err)
	}
}

func ExampleEmailsService_Batch() {
	ctx := context.Background()
	client, err := runearc.New("mb_live_xxxxxxxxx")
	if err != nil {
		log.Fatal(err)
	}
	emails := []runearc.SendEmailRequest{
		{From: "Acme <noreply@acme.com>", To: []string{"bob@example.com"}, Subject: "Hi Bob", Text: "..."},
		{From: "Acme <noreply@acme.com>", To: []string{"carol@example.com"}, Subject: "Hi Carol", Text: "..."},
	}
	res, err := client.Emails.Batch(ctx, emails, runearc.WithBatchValidation(runearc.BatchPermissive))
	if err != nil {
		log.Fatal(err)
	}
	for _, item := range res.Errors {
		fmt.Println(item.Index, item.Code, item.Message)
	}
}

func ExampleMessagesService_Wait() {
	ctx := context.Background()
	client, err := runearc.New("mb_live_xxxxxxxxx")
	if err != nil {
		log.Fatal(err)
	}
	inbox, err := client.Inboxes.Create(ctx, runearc.CreateInboxRequest{LocalPart: "signup-bot", DisplayName: "Signup bot"})
	if err != nil {
		log.Fatal(err)
	}
	// Wait up to a minute for a matching message. A nil result means none arrived.
	got, err := client.Messages.Wait(ctx, inbox.ID, runearc.WaitParams{From: "github.com", Subject: "verify", Timeout: time.Minute})
	if err != nil {
		log.Fatal(err)
	}
	if got != nil {
		fmt.Println(got.Codes, got.Links, got.Message.Subject)
	}
}

func ExampleConversationsService_Update() {
	ctx := context.Background()
	client, err := runearc.New("mb_live_xxxxxxxxx")
	if err != nil {
		log.Fatal(err)
	}
	page, err := client.Conversations.List(ctx, runearc.ConversationFilter{Status: "open", Assignee: "none", Q: "refund"})
	if err != nil {
		log.Fatal(err)
	}
	id := page.Data[0].ID
	// Set sends a value, Clear sends null, and a nil field is left alone.
	_, err = client.Conversations.Update(ctx, id, runearc.ConversationUpdate{
		Status:       "closed",
		SnoozedUntil: runearc.Clear[time.Time](),
	})
	if err != nil {
		log.Fatal(err)
	}
}

func ExampleError() {
	// A stand in API that refuses the request, so this example runs anywhere.
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":{"code":"sender_not_verified","message":"acme.org is not verified","fix":"Verify the domain first"}}`)
	}))
	defer api.Close()

	client, err := runearc.New("mb_test", runearc.WithBaseURL(api.URL))
	if err != nil {
		log.Fatal(err)
	}
	_, err = client.Emails.Send(context.Background(), &runearc.SendEmailRequest{
		From: "noreply@acme.org", To: []string{"bob@example.com"}, Subject: "Hi", Text: "Hi",
	})
	var apiErr *runearc.Error
	if errors.As(err, &apiErr) {
		fmt.Println(apiErr.Status, apiErr.Code, apiErr.Kind)
		fmt.Println(apiErr.Message)
		fmt.Println(apiErr.Fix)
	}
	// Output:
	// 403 sender_not_verified forbidden
	// acme.org is not verified
	// Verify the domain first
}

func ExampleVerifyWebhook() {
	// A delivery signed with the endpoint's secret.
	event, err := runearc.VerifyWebhook(runearc.VerifyWebhookInput{
		Payload: []byte(`{"id":"evt_1","type":"message.received","timestamp":"2026-10-14T10:00:00Z","data":{"inbox_id":"ibx_1"}}`),
		Headers: http.Header{
			"Webhook-Id":        {"evt_1"},
			"Webhook-Timestamp": {"1760436000"},
			"Webhook-Signature": {"v1,GUKO0LvK0gXaqD+G9E+8tsuC62ZGWg/bzsQ+kUd1+/A="},
		},
		Secret: "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
		Now:    time.Unix(1760436000, 0),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(event.Type, event.ID)
	// Output: message.received evt_1
}

func ExampleVerifyWebhook_handler() {
	secret := "whsec_..." // from Webhooks.Create, shown once
	http.HandleFunc("/runearc", func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body) // the raw body, not decoded and encoded again
		if err != nil {
			http.Error(w, "unreadable body", http.StatusBadRequest)
			return
		}
		event, err := runearc.VerifyWebhook(runearc.VerifyWebhookInput{Payload: payload, Headers: r.Header, Secret: secret})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Println(event.Type, event.ID) // event.ID is the webhook-id: use it to drop duplicates
		w.WriteHeader(http.StatusNoContent)
	})
}
