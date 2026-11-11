// Package runearc is the official Go SDK for the Runearc email API.
//
// Send, batch and schedule email, create inboxes your agents can read, manage
// domains and verify webhooks.
//
// It uses the standard library only. Create a [Client] with [New]; every method
// takes a [context.Context] first and returns a typed result or an *[Error]:
//
//	client, err := runearc.New(os.Getenv("RUNEARC_API_KEY"))
//	if err != nil {
//		log.Fatal(err)
//	}
//	sent, err := client.Emails.Send(ctx, &runearc.SendEmailRequest{
//		From:    "Acme <noreply@acme.com>",
//		To:      []string{"bob@example.com"},
//		Subject: "Welcome to Acme",
//		HTML:    "<p>Thanks for signing up.</p>",
//	})
//
// Field names are the API's JSON in snake case on the wire and CamelCase in Go.
// Requests that fail with 429, or with 5xx or a network error when repeating
// them is safe, are retried with backoff; calls that send mail carry an
// Idempotency-Key so a retry never sends twice.
package runearc
