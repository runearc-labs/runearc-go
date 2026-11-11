package runearc

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// InboxesService creates and manages inboxes.
type InboxesService struct{ c *Client }

// Create creates an inbox. The zero request creates one with a random address.
func (s *InboxesService) Create(ctx context.Context, req CreateInboxRequest, opts ...RequestOption) (*Inbox, error) {
	var out Inbox
	err := s.c.doJSON(ctx, call{method: http.MethodPost, path: "/v1/inboxes", body: req}, opts, &out)
	return orNil(&out, err)
}

// List returns every inbox.
func (s *InboxesService) List(ctx context.Context, opts ...RequestOption) (*List[Inbox], error) {
	var out List[Inbox]
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/inboxes"}, opts, &out)
	return orNil(&out, err)
}

// Get returns one inbox.
func (s *InboxesService) Get(ctx context.Context, id string, opts ...RequestOption) (*Inbox, error) {
	var out Inbox
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/inboxes/" + seg(id)}, opts, &out)
	return orNil(&out, err)
}

// Delete deletes an inbox.
func (s *InboxesService) Delete(ctx context.Context, id string, opts ...RequestOption) error {
	return s.c.doJSON(ctx, call{method: http.MethodDelete, path: "/v1/inboxes/" + seg(id)}, opts, nil)
}

// MessagesService reads, waits for, sends and replies to messages.
type MessagesService struct{ c *Client }

// List returns a page of the messages of an inbox, newest first.
func (s *MessagesService) List(ctx context.Context, inboxID string, params PageParams, opts ...RequestOption) (*Page[Message], error) {
	var out Page[Message]
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/inboxes/" + seg(inboxID) + "/messages", query: params.values()}, opts, &out)
	return orNil(&out, err)
}

// Get returns one message.
func (s *MessagesService) Get(ctx context.Context, id string, opts ...RequestOption) (*Message, error) {
	var out Message
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/messages/" + seg(id)}, opts, &out)
	return orNil(&out, err)
}

// Raw returns the message as received (RFC 822).
func (s *MessagesService) Raw(ctx context.Context, id string, opts ...RequestOption) (string, error) {
	raw, _, err := s.c.send(ctx, call{method: http.MethodGet, path: "/v1/messages/" + seg(id) + "/raw"}, opts)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// Wait blocks until a matching message arrives, up to params.Timeout (default 30
// seconds, at most 120), and returns it with the codes and links found in it.
// It returns nil and no error when nothing arrived in time; calling again is
// safe. The context can end the wait sooner.
func (s *MessagesService) Wait(ctx context.Context, inboxID string, params WaitParams, opts ...RequestOption) (*Waited, error) {
	timeout := params.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	q := url.Values{}
	q.Set("timeout", strconv.Itoa(max(1, int(timeout.Round(time.Second)/time.Second))))
	if params.From != "" {
		q.Set("from", params.From)
	}
	if params.Subject != "" {
		q.Set("subject", params.Subject)
	}
	if params.To != "" {
		q.Set("to", params.To)
	}
	if !params.After.IsZero() {
		q.Set("after", params.After.UTC().Format(time.RFC3339Nano))
	}
	var out Waited
	err := s.c.doJSON(ctx, call{
		method:  http.MethodGet,
		path:    "/v1/inboxes/" + seg(inboxID) + "/wait",
		query:   q,
		timeout: timeout + 15*time.Second,
	}, opts, &out)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Code == "wait_timeout" {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

// Send sends a new message from an inbox. It gets an Idempotency-Key
// automatically unless [WithIdempotencyKey] gives one.
func (s *MessagesService) Send(ctx context.Context, inboxID string, req *SendMessageRequest, opts ...RequestOption) (*Message, error) {
	var out Message
	err := s.c.doJSON(ctx, call{method: http.MethodPost, path: "/v1/inboxes/" + seg(inboxID) + "/messages", body: req, sendsMail: true}, opts, &out)
	return orNil(&out, err)
}

// Reply replies in the thread of a message. It gets an Idempotency-Key
// automatically unless [WithIdempotencyKey] gives one.
func (s *MessagesService) Reply(ctx context.Context, messageID string, req *ReplyRequest, opts ...RequestOption) (*Message, error) {
	var out Message
	err := s.c.doJSON(ctx, call{method: http.MethodPost, path: "/v1/messages/" + seg(messageID) + "/reply", body: req, sendsMail: true}, opts, &out)
	return orNil(&out, err)
}

// ThreadsService reads threads.
type ThreadsService struct{ c *Client }

// List returns the threads of an inbox.
func (s *ThreadsService) List(ctx context.Context, inboxID string, opts ...RequestOption) (*List[Thread], error) {
	var out List[Thread]
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/inboxes/" + seg(inboxID) + "/threads"}, opts, &out)
	return orNil(&out, err)
}

// Get returns a thread with its messages.
func (s *ThreadsService) Get(ctx context.Context, id string, opts ...RequestOption) (*ThreadWithMessages, error) {
	var out ThreadWithMessages
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/threads/" + seg(id)}, opts, &out)
	return orNil(&out, err)
}
