package runearc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	runearc "github.com/runearc-labs/runearc-go"
)

func TestInboxesMessagesThreadsConversations(t *testing.T) {
	ctx := context.Background()
	c := client(t)
	local := fmt.Sprintf("sdk-%d", time.Now().UnixNano())
	inbox, err := c.Inboxes.Create(ctx, runearc.CreateInboxRequest{LocalPart: local, DisplayName: "SDK"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(inbox.ID, "ibx_") || inbox.LocalPart != local || !strings.HasSuffix(inbox.Domain, stack.sharedDomain) {
		t.Fatalf("inbox %+v", inbox)
	}
	got, err := c.Inboxes.Get(ctx, inbox.ID)
	if err != nil || got.Address != inbox.Address {
		t.Fatalf("get: %v %+v", err, got)
	}
	inboxes, err := c.Inboxes.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFunc(inboxes.Data, func(i runearc.Inbox) bool { return i.ID == inbox.ID }) {
		t.Fatal("inbox is not in the list")
	}

	subject := unique("inbox send")
	sent, err := c.Messages.Send(ctx, inbox.ID, &runearc.SendMessageRequest{To: []string{testAddress("delivered")}, Subject: subject, Text: "Hello from an inbox"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sent.ID, "msg_") || sent.Subject != subject {
		t.Fatalf("sent %+v", sent)
	}

	// Same key twice: one message.
	key := randomHex(16)
	req := &runearc.SendMessageRequest{To: []string{testAddress("delivered")}, Subject: unique("inbox idem"), Text: "x"}
	a, err := c.Messages.Send(ctx, inbox.ID, req, runearc.WithIdempotencyKey(key))
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Messages.Send(ctx, inbox.ID, req, runearc.WithIdempotencyKey(key))
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Fatalf("ids differ: %s %s", a.ID, b.ID)
	}

	page, err := c.Messages.List(ctx, inbox.ID, runearc.PageParams{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !containsFunc(page.Data, func(m runearc.Message) bool { return m.ID == sent.ID }) {
		t.Fatal("message is not in the list")
	}
	msg, err := c.Messages.Get(ctx, sent.ID)
	if err != nil || msg.ThreadID != sent.ThreadID {
		t.Fatalf("get: %v %+v", err, msg)
	}
	raw, err := c.Messages.Raw(ctx, sent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "Subject: "+subject) {
		t.Fatalf("raw message has no subject line:\n%s", raw)
	}

	reply, err := c.Messages.Reply(ctx, sent.ID, &runearc.ReplyRequest{Text: "A follow up"})
	if err != nil {
		t.Fatal(err)
	}
	if reply.ThreadID != sent.ThreadID || reply.InReplyTo == nil || sent.RFCMessageID == nil || *reply.InReplyTo != *sent.RFCMessageID {
		t.Fatalf("reply %+v", reply)
	}

	threads, err := c.Threads.List(ctx, inbox.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFunc(threads.Data, func(th runearc.Thread) bool { return th.ID == sent.ThreadID }) {
		t.Fatal("thread is not in the list")
	}
	thread, err := c.Threads.Get(ctx, sent.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, m := range thread.Messages {
		ids[m.ID] = true
	}
	if !ids[sent.ID] || !ids[reply.ID] {
		t.Fatalf("thread messages %v", ids)
	}

	// Nothing inbound arrives (no SMTP server in this run): Wait returns nil.
	waited, err := c.Messages.Wait(ctx, inbox.ID, runearc.WaitParams{Timeout: time.Second})
	if err != nil || waited != nil {
		t.Fatalf("wait: %v %+v", err, waited)
	}

	if _, err := c.Conversations.List(ctx, runearc.ConversationFilter{InboxID: inbox.ID, Status: "all"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Conversations.Counts(ctx); err != nil {
		t.Fatal(err)
	}
	closed, err := c.Conversations.Update(ctx, sent.ThreadID, runearc.ConversationUpdate{Status: "closed"})
	if err != nil || closed.Status != "closed" {
		t.Fatalf("close: %v %+v", err, closed)
	}
	reopened, err := c.Conversations.Update(ctx, sent.ThreadID, runearc.ConversationUpdate{Status: "open"})
	if err != nil || reopened.Status != "open" {
		t.Fatalf("reopen: %v %+v", err, reopened)
	}
	until := time.Now().Add(time.Hour)
	snoozed, err := c.Conversations.Update(ctx, sent.ThreadID, runearc.ConversationUpdate{SnoozedUntil: runearc.Set(until)})
	if err != nil || snoozed.SnoozedUntil == nil {
		t.Fatalf("snooze: %v %+v", err, snoozed)
	}
	if d := snoozed.SnoozedUntil.Sub(until); d < -time.Second || d > time.Second {
		t.Fatalf("snoozed_until %s, want about %s", snoozed.SnoozedUntil, until)
	}
	woken, err := c.Conversations.Update(ctx, sent.ThreadID, runearc.ConversationUpdate{SnoozedUntil: runearc.Clear[time.Time](), AssigneeUserID: runearc.Clear[string]()})
	if err != nil || woken.SnoozedUntil != nil || woken.Assignee != nil {
		t.Fatalf("wake: %v %+v", err, woken)
	}
	if err := c.Conversations.Read(ctx, sent.ThreadID); err != nil {
		t.Fatal(err)
	}
	if err := c.Conversations.Unread(ctx, sent.ThreadID); err != nil {
		t.Fatal(err)
	}

	if err := c.Inboxes.Delete(ctx, inbox.ID); err != nil {
		t.Fatal(err)
	}
	_, err = c.Inboxes.Get(ctx, inbox.ID)
	if e := apiError(t, err); e.Status != 404 {
		t.Fatalf("got %+v", e)
	}
}

func TestConversationUpdateEncoding(t *testing.T) {
	until := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		update runearc.ConversationUpdate
		want   string
	}{
		{"empty", runearc.ConversationUpdate{}, `{}`},
		{"status", runearc.ConversationUpdate{Status: "closed"}, `{"status":"closed"}`},
		{"assign", runearc.ConversationUpdate{AssigneeUserID: runearc.Set("usr_1")}, `{"assignee_user_id":"usr_1"}`},
		{"unassign", runearc.ConversationUpdate{AssigneeUserID: runearc.Clear[string]()}, `{"assignee_user_id":null}`},
		{"snooze", runearc.ConversationUpdate{SnoozedUntil: runearc.Set(until)}, `{"snoozed_until":"2026-10-14T09:00:00Z"}`},
		{"wake", runearc.ConversationUpdate{SnoozedUntil: runearc.Clear[time.Time]()}, `{"snoozed_until":null}`},
	} {
		got, err := json.Marshal(tc.update)
		if err != nil || string(got) != tc.want {
			t.Errorf("%s: got %s (%v), want %s", tc.name, got, err, tc.want)
		}
	}
}

func TestDomains(t *testing.T) {
	ctx := context.Background()
	c := client(t)
	name := fmt.Sprintf("sdk-crud-%d.example.com", time.Now().UnixNano())
	added, err := c.Domains.Create(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if added.Name != name || added.Status != "pending" || len(added.Records) == 0 {
		t.Fatalf("added %+v", added)
	}
	list, err := c.Domains.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFunc(list.Data, func(d runearc.Domain) bool { return d.ID == added.ID }) {
		t.Fatal("domain is not in the list")
	}
	byID, err := c.Domains.Get(ctx, added.ID)
	if err != nil || byID.Name != name {
		t.Fatalf("get by id: %v %+v", err, byID)
	}
	byName, err := c.Domains.Get(ctx, name)
	if err != nil || byName.ID != added.ID {
		t.Fatalf("get by name: %v %+v", err, byName)
	}
	checked, err := c.Domains.Verify(ctx, added.ID)
	if err != nil || checked.ID != added.ID || checked.Status == "verified" {
		t.Fatalf("verify: %v %+v", err, checked)
	}
	if err := c.Domains.Delete(ctx, added.ID); err != nil {
		t.Fatal(err)
	}
	_, err = c.Domains.Get(ctx, added.ID)
	if e := apiError(t, err); e.Status != 404 {
		t.Fatalf("got %+v", e)
	}
}

func TestKeys(t *testing.T) {
	ctx := context.Background()
	c := client(t)
	created, err := c.Keys.Create(ctx, runearc.CreateKeyRequest{Name: "sdk sending", Permissions: "sending", DomainID: stack.domainID})
	if err != nil {
		t.Fatal(err)
	}
	if created.Secret == "" || created.Key.Permissions != "sending" {
		t.Fatalf("created %+v", created.Key)
	}
	list, err := c.Keys.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFunc(list.Data, func(k runearc.APIKey) bool { return k.ID == created.Key.ID }) {
		t.Fatal("key is not in the list")
	}

	sender := clientWithKey(t, created.Secret)
	sent, err := sender.Emails.Send(ctx, &runearc.SendEmailRequest{From: "app@" + stack.domain, To: []string{testAddress("delivered")}, Subject: unique("sending key"), Text: "x"})
	if err != nil || !strings.HasPrefix(sent.ID, "eml_") {
		t.Fatalf("send: %v %+v", err, sent)
	}
	// A sending key cannot manage inboxes.
	_, err = sender.Inboxes.Create(ctx, runearc.CreateInboxRequest{})
	if e := apiError(t, err); e.Status != 403 {
		t.Fatalf("got %+v", e)
	}

	if err := c.Keys.Delete(ctx, created.Key.ID); err != nil {
		t.Fatal(err)
	}
	_, err = sender.Emails.List(ctx, runearc.PageParams{})
	if e := apiError(t, err); e.Status != 401 {
		t.Fatalf("got %+v", e)
	}
}

func TestSuppressionsAndEvents(t *testing.T) {
	ctx := context.Background()
	c := client(t)
	// The API has no POST for suppressions (bounces and complaints add them, and
	// simulated test recipients are never suppressed), so one row is added with SQL,
	// the way core's add_suppression stores it.
	sent, err := c.Emails.Send(ctx, &runearc.SendEmailRequest{From: "app@" + stack.domain, To: []string{testAddress("delivered")}, Subject: unique("events"), Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	email, err := c.Emails.Get(ctx, sent.ID)
	if err != nil {
		t.Fatal(err)
	}
	address := fmt.Sprintf("blocked-%d@example.org", time.Now().UnixNano())
	if out, err := psql(stack.dbURL, fmt.Sprintf(
		"INSERT INTO suppressions (org_id, project_id, address, reason) VALUES ('%s', '%s', '%s', 'manual')",
		stack.orgID, email.ProjectID, address)); err != nil {
		t.Fatalf("psql: %v: %s", err, out)
	}
	list, err := c.Suppressions.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range list.Data {
		if s.Address == address {
			found = s.Reason == "manual"
		}
	}
	if !found {
		t.Fatal("suppression is not in the list")
	}
	if err := c.Suppressions.Delete(ctx, address); err != nil {
		t.Fatal(err)
	}
	list, err = c.Suppressions.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if containsFunc(list.Data, func(s runearc.Suppression) bool { return s.Address == address }) {
		t.Fatal("suppression was not removed")
	}

	first, err := c.Events.List(ctx, runearc.EventsParams{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Data) == 0 || !containsFunc(first.Data, func(e runearc.Event) bool { return strings.HasPrefix(e.Type, "message.") }) {
		t.Fatalf("events %+v", first.Data)
	}
	lastID := first.Data[len(first.Data)-1].ID
	if first.NextAfter == nil || *first.NextAfter != lastID {
		t.Fatalf("next_after %v, want %s", first.NextAfter, lastID)
	}
	next, err := c.Events.List(ctx, runearc.EventsParams{After: *first.NextAfter, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range next.Data {
		if e.ID <= *first.NextAfter {
			t.Fatalf("event %s is not after %s", e.ID, *first.NextAfter)
		}
	}
}

func containsFunc[T any](s []T, f func(T) bool) bool {
	for _, v := range s {
		if f(v) {
			return true
		}
	}
	return false
}
