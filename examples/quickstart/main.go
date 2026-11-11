// Command quickstart creates an inbox, sends to it, waits for the message and verifies a webhook.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	runearc "github.com/runearc-labs/runearc-go"
)

func main() {
	ctx := context.Background()
	client, err := runearc.New("") // reads RUNEARC_API_KEY and RUNEARC_BASE_URL
	check(err)

	inbox, err := client.Inboxes.Create(ctx, runearc.CreateInboxRequest{DisplayName: "quickstart"})
	check(err)
	fmt.Println("inbox", inbox.Address)

	subject := fmt.Sprintf("Hello from the Go SDK %d", time.Now().UnixNano()%1_000_000)
	sent, err := client.Emails.Send(ctx, &runearc.SendEmailRequest{
		From: os.Getenv("RUNEARC_FROM"), To: []string{inbox.Address}, Subject: subject, Text: "It works.",
	})
	check(err)
	fmt.Println("sent", sent.ID)

	waited, err := client.Messages.Wait(ctx, inbox.ID, runearc.WaitParams{Subject: subject, After: inbox.CreatedAt, Timeout: 30 * time.Second})
	check(err)
	if waited == nil {
		log.Fatal("no message arrived within 30 seconds")
	}
	fmt.Println("received", waited.Message.Subject)

	// Sign a sample event the way Runearc does (Standard Webhooks), then verify it.
	raw := make([]byte, 32)
	_, err = rand.Read(raw)
	check(err)
	secret := "whsec_" + base64.StdEncoding.EncodeToString(raw)
	payload := fmt.Sprintf(`{"id":"evt_1","type":"message.delivered","data":{"email_id":%q}}`, sent.ID)
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, raw)
	fmt.Fprintf(mac, "evt_1.%d.%s", ts, payload)
	headers := http.Header{}
	headers.Set("Webhook-Id", "evt_1")
	headers.Set("Webhook-Timestamp", fmt.Sprint(ts))
	headers.Set("Webhook-Signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	_, err = runearc.VerifyWebhook(runearc.VerifyWebhookInput{Payload: []byte(payload), Headers: headers, Secret: secret})
	check(err)
	fmt.Println("webhook verified")

	check(client.Inboxes.Delete(ctx, inbox.ID))
}

func check(err error) {
	if err != nil {
		log.Fatal(strings.TrimSpace(err.Error()))
	}
}
