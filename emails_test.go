package runearc_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	runearc "github.com/runearc-labs/runearc-go"
)

func TestEmailsSend(t *testing.T) {
	ctx := context.Background()
	c := client(t)
	subject := unique("send")
	sent, err := c.Emails.Send(ctx, &runearc.SendEmailRequest{
		From:    from(),
		To:      []string{testAddress("delivered")},
		Subject: subject,
		HTML:    "<p>Hello</p>",
		Text:    "Hello",
		Tags:    []runearc.Tag{{Name: "kind", Value: "test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sent.ID, "eml_") {
		t.Fatalf("id %q", sent.ID)
	}
	var got *runearc.EmailDetail
	eventually(t, 15*time.Second, func() (bool, error) {
		e, err := c.Emails.Get(ctx, sent.ID)
		if err != nil {
			return false, err
		}
		got = e
		return e.Status == runearc.EmailDelivered, nil
	})
	if got.Object != "email" || got.Subject != subject {
		t.Fatalf("got %+v", got)
	}
	if len(got.To) != 1 || got.To[0] != testAddress("delivered") {
		t.Fatalf("to %v", got.To)
	}
	if len(got.Recipients) == 0 || got.Recipients[0].Status != "delivered" {
		t.Fatalf("recipients %+v", got.Recipients)
	}
	if len(got.Tags) != 1 || got.Tags[0] != (runearc.Tag{Name: "kind", Value: "test"}) {
		t.Fatalf("tags %v", got.Tags)
	}
	if got.Text == nil || *got.Text != "Hello" {
		t.Fatalf("text %v", got.Text)
	}

	page, err := c.Emails.List(ctx, runearc.PageParams{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range page.Data {
		found = found || e.ID == sent.ID
	}
	if !found {
		t.Fatal("sent email is not in the list")
	}
}

func TestEmailsListPagination(t *testing.T) {
	ctx := context.Background()
	c := client(t)
	for i := 0; i < 2; i++ {
		if _, err := c.Emails.Send(ctx, &runearc.SendEmailRequest{From: from(), To: []string{testAddress("delivered")}, Subject: unique("page"), Text: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := c.Emails.List(ctx, runearc.PageParams{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Data) != 1 || first.NextCursor == nil {
		t.Fatalf("first page %+v", first)
	}
	next, err := c.Emails.List(ctx, runearc.PageParams{Limit: 1, Cursor: *first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Data) != 1 || next.Data[0].ID == first.Data[0].ID {
		t.Fatalf("second page %+v", next)
	}
}

func TestEmailsIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	c := client(t)
	key := randomHex(16)
	req := &runearc.SendEmailRequest{From: from(), To: []string{testAddress("delivered")}, Subject: unique("idem"), Text: "once"}
	a, err := c.Emails.Send(ctx, req, runearc.WithIdempotencyKey(key))
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Emails.Send(ctx, req, runearc.WithIdempotencyKey(key))
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Fatalf("ids differ: %s %s", a.ID, b.ID)
	}
	different := *req
	different.Text = "different"
	_, err = c.Emails.Send(ctx, &different, runearc.WithIdempotencyKey(key))
	e := apiError(t, err)
	if e.Status != 409 || e.Code != "idempotency_conflict" || e.Kind != runearc.KindConflict {
		t.Fatalf("got %+v", e)
	}
}

func TestTypedErrors(t *testing.T) {
	ctx := context.Background()
	c := client(t)
	_, err := c.Emails.Send(ctx, &runearc.SendEmailRequest{From: "nobody@not-my-domain.example", To: []string{testAddress("delivered")}, Subject: "x", Text: "x"})
	e := apiError(t, err)
	if e.Status != 403 || e.Code != "sender_not_verified" || e.Kind != runearc.KindForbidden || e.Fix == "" || e.Message == "" {
		t.Fatalf("got %+v", e)
	}
	if !strings.Contains(e.Error(), "sender_not_verified") || !strings.Contains(e.Error(), "403") {
		t.Fatalf("Error() = %q", e.Error())
	}

	bad := clientWithKey(t, "mb_live_not_a_real_key")
	_, err = bad.Emails.List(ctx, runearc.PageParams{})
	e = apiError(t, err)
	if e.Status != 401 || e.Code != "invalid_api_key" || e.Kind != runearc.KindUnauthorized || e.Fix == "" {
		t.Fatalf("got %+v", e)
	}

	_, err = c.Emails.Get(ctx, "eml_00000000000000000000000000")
	e = apiError(t, err)
	if e.Status != 404 || e.Kind != runearc.KindNotFound {
		t.Fatalf("got %+v", e)
	}
}

func TestSendersListsVerifiedDomain(t *testing.T) {
	out, err := client(t).Senders.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range out.Data {
		if s.Domain == stack.domain && s.Verified {
			return
		}
	}
	t.Fatalf("no verified sender for %s in %+v", stack.domain, out.Data)
}

func TestBatch(t *testing.T) {
	ctx := context.Background()
	c := client(t)
	item := func(subject string) runearc.SendEmailRequest {
		return runearc.SendEmailRequest{From: from(), To: []string{testAddress("delivered")}, Subject: subject, Text: "hi"}
	}

	t.Run("sends every valid item and returns ids in order", func(t *testing.T) {
		subjects := []string{unique("batch a"), unique("batch b"), unique("batch c")}
		var items []runearc.SendEmailRequest
		for _, s := range subjects {
			items = append(items, item(s))
		}
		res, err := c.Emails.Batch(ctx, items)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Data) != 3 || len(res.Errors) != 0 {
			t.Fatalf("got %+v", res)
		}
		for i, r := range res.Data {
			e, err := c.Emails.Get(ctx, r.ID)
			if err != nil {
				t.Fatal(err)
			}
			if e.Subject != subjects[i] {
				t.Fatalf("item %d has subject %q, want %q", i, e.Subject, subjects[i])
			}
		}
	})

	t.Run("strict: one bad item refuses the whole batch with errors", func(t *testing.T) {
		subject := unique("strict")
		bad := item(subject)
		bad.From = "nobody@not-my-domain.example"
		_, err := c.Emails.Batch(ctx, []runearc.SendEmailRequest{item(subject), bad, item(subject)})
		e := apiError(t, err)
		if e.Status != 400 || e.Code != "invalid_batch" || !strings.Contains(e.Message, "emails[1]") {
			t.Fatalf("got %+v", e)
		}
		if len(e.Errors) != 1 || e.Errors[0].Index != 1 || e.Errors[0].Code == "" {
			t.Fatalf("errors %+v", e.Errors)
		}
		page, err := c.Emails.List(ctx, runearc.PageParams{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Data {
			if e.Subject == subject {
				t.Fatal("a refused batch stored an email")
			}
		}
	})

	t.Run("permissive: valid items send, invalid ones come back in errors", func(t *testing.T) {
		bad := item("bad")
		bad.From = "nobody@not-my-domain.example"
		res, err := c.Emails.Batch(ctx, []runearc.SendEmailRequest{item(unique("perm")), bad}, runearc.WithBatchValidation(runearc.BatchPermissive))
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Data) != 1 || len(res.Errors) != 1 || res.Errors[0].Index != 1 {
			t.Fatalf("got %+v", res)
		}
	})

	t.Run("repeats with the same idempotency key return the same ids", func(t *testing.T) {
		items := []runearc.SendEmailRequest{item(unique("batch idem"))}
		key := randomHex(16)
		a, err := c.Emails.Batch(ctx, items, runearc.WithIdempotencyKey(key))
		if err != nil {
			t.Fatal(err)
		}
		b, err := c.Emails.Batch(ctx, items, runearc.WithIdempotencyKey(key))
		if err != nil {
			t.Fatal(err)
		}
		if len(a.Data) != 1 || len(b.Data) != 1 || a.Data[0] != b.Data[0] {
			t.Fatalf("a=%+v b=%+v", a, b)
		}
	})
}

func TestScheduledEmails(t *testing.T) {
	ctx := context.Background()
	c := client(t)

	t.Run("schedules, reschedules and cancels", func(t *testing.T) {
		when := time.Now().Add(time.Hour)
		sent, err := c.Emails.Send(ctx, &runearc.SendEmailRequest{
			From: from(), To: []string{testAddress("delivered")}, Subject: unique("scheduled"), Text: "later",
			ScheduledAt: when.Format(time.RFC3339),
		})
		if err != nil {
			t.Fatal(err)
		}
		e, err := c.Emails.Get(ctx, sent.ID)
		if err != nil {
			t.Fatal(err)
		}
		if e.Status != runearc.EmailScheduled || e.LastEvent != "scheduled" || e.ScheduledAt == nil {
			t.Fatalf("got %+v", e)
		}
		if d := e.ScheduledAt.Sub(when); d < -time.Second || d > time.Second {
			t.Fatalf("scheduled_at %s, want about %s", e.ScheduledAt, when)
		}

		later := time.Now().Add(2 * time.Hour)
		ref, err := c.Emails.Reschedule(ctx, sent.ID, later.Format(time.RFC3339))
		if err != nil {
			t.Fatal(err)
		}
		if *ref != (runearc.EmailRef{Object: "email", ID: sent.ID}) {
			t.Fatalf("ref %+v", ref)
		}
		after, err := c.Emails.Get(ctx, sent.ID)
		if err != nil {
			t.Fatal(err)
		}
		if d := after.ScheduledAt.Sub(later); d < -time.Second || d > time.Second {
			t.Fatalf("scheduled_at %s, want about %s", after.ScheduledAt, later)
		}

		canceled, err := c.Emails.Cancel(ctx, sent.ID)
		if err != nil {
			t.Fatal(err)
		}
		if canceled.ID != sent.ID || canceled.Object != "email" {
			t.Fatalf("canceled %+v", canceled)
		}
		final, err := c.Emails.Get(ctx, sent.ID)
		if err != nil {
			t.Fatal(err)
		}
		if final.Status != runearc.EmailCanceled {
			t.Fatalf("status %s", final.Status)
		}

		_, err = c.Emails.Update(ctx, sent.ID, runearc.UpdateEmailRequest{ScheduledAt: "in 1 hour"})
		e2 := apiError(t, err)
		if e2.Status != 409 || e2.Code != "email_not_scheduled" {
			t.Fatalf("got %+v", e2)
		}
	})

	t.Run("accepts the relative form and refuses times without an offset", func(t *testing.T) {
		sent, err := c.Emails.Send(ctx, &runearc.SendEmailRequest{
			From: from(), To: []string{testAddress("delivered")}, Subject: unique("relative"), Text: "later", ScheduledAt: "in 2 hours",
		})
		if err != nil {
			t.Fatal(err)
		}
		e, err := c.Emails.Get(ctx, sent.ID)
		if err != nil {
			t.Fatal(err)
		}
		if e.Status != runearc.EmailScheduled {
			t.Fatalf("status %s", e.Status)
		}
		if _, err := c.Emails.Cancel(ctx, sent.ID); err != nil {
			t.Fatal(err)
		}

		_, err = c.Emails.Send(ctx, &runearc.SendEmailRequest{
			From: from(), To: []string{testAddress("delivered")}, Subject: "x", Text: "x", ScheduledAt: "tomorrow at 9am",
		})
		bad := apiError(t, err)
		if bad.Status != 400 || bad.Code != "invalid_scheduled_at" || bad.Fix == "" {
			t.Fatalf("got %+v", bad)
		}
	})

	t.Run("refuses to cancel an email that was sent", func(t *testing.T) {
		sent, err := c.Emails.Send(ctx, &runearc.SendEmailRequest{From: from(), To: []string{testAddress("delivered")}, Subject: unique("now"), Text: "now"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Emails.Cancel(ctx, sent.ID)
		e := apiError(t, err)
		if e.Status != 409 || e.Code != "email_not_scheduled" {
			t.Fatalf("got %+v", e)
		}
	})
}

func TestSendAcceptsHeadersAttachmentsAndReplyTo(t *testing.T) {
	ctx := context.Background()
	c := client(t)
	sent, err := c.Emails.Send(ctx, &runearc.SendEmailRequest{
		From:    from(),
		To:      []string{testAddress("delivered")},
		ReplyTo: []string{"support@" + stack.domain},
		Subject: unique("fields"),
		Text:    "x",
		Headers: map[string]string{"X-Entity-Ref-ID": "123"},
		Attachments: []runearc.EmailAttachmentInput{
			{Filename: "hello.txt", Content: "aGVsbG8="},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	e, err := c.Emails.Get(ctx, sent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Attachments) != 1 || e.Attachments[0].Filename != "hello.txt" || len(e.ReplyTo) != 1 || e.Headers["X-Entity-Ref-ID"] != "123" {
		t.Fatalf("got %+v", e)
	}
}

func TestContextCancellation(t *testing.T) {
	c := client(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Emails.List(ctx, runearc.PageParams{})
	e := apiError(t, err)
	if e.Code != "aborted" || e.Kind != runearc.KindCanceled || e.Status != 0 {
		t.Fatalf("got %+v", e)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("errors.Is(err, context.Canceled) is false")
	}
}
