package runearc

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// ConversationsService is the shared support view across every inbox you can read.
type ConversationsService struct{ c *Client }

// List returns a page of conversations.
func (s *ConversationsService) List(ctx context.Context, filter ConversationFilter, opts ...RequestOption) (*Page[Conversation], error) {
	q := url.Values{}
	for k, v := range map[string]string{
		"status":     filter.Status,
		"assignee":   filter.Assignee,
		"project_id": filter.ProjectID,
		"inbox_id":   filter.InboxID,
		"domain":     filter.Domain,
		"q":          filter.Q,
		"cursor":     filter.Cursor,
	} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if filter.Limit > 0 {
		q.Set("limit", strconv.Itoa(filter.Limit))
	}
	var out Page[Conversation]
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/conversations", query: q}, opts, &out)
	return orNil(&out, err)
}

// Counts returns the open and unread conversations, in total and per project and inbox.
func (s *ConversationsService) Counts(ctx context.Context, opts ...RequestOption) (*ConversationCounts, error) {
	var out ConversationCounts
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/conversations/counts"}, opts, &out)
	return orNil(&out, err)
}

// Update changes the status, assignee or snooze time of a conversation.
func (s *ConversationsService) Update(ctx context.Context, id string, update ConversationUpdate, opts ...RequestOption) (*Conversation, error) {
	var out Conversation
	err := s.c.doJSON(ctx, call{method: http.MethodPatch, path: "/v1/conversations/" + seg(id), body: update}, opts, &out)
	return orNil(&out, err)
}

// Read marks a conversation as read.
func (s *ConversationsService) Read(ctx context.Context, id string, opts ...RequestOption) error {
	return s.c.doJSON(ctx, call{method: http.MethodPost, path: "/v1/conversations/" + seg(id) + "/read", idempotent: true}, opts, nil)
}

// Unread marks a conversation as unread.
func (s *ConversationsService) Unread(ctx context.Context, id string, opts ...RequestOption) error {
	return s.c.doJSON(ctx, call{method: http.MethodDelete, path: "/v1/conversations/" + seg(id) + "/read"}, opts, nil)
}
