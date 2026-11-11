package runearc

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// KeysService manages API keys.
type KeysService struct{ c *Client }

// Create creates a key. The response has its Secret, shown once.
func (s *KeysService) Create(ctx context.Context, req CreateKeyRequest, opts ...RequestOption) (*CreatedKey, error) {
	var out CreatedKey
	err := s.c.doJSON(ctx, call{method: http.MethodPost, path: "/v1/keys", body: req}, opts, &out)
	return orNil(&out, err)
}

// List returns every key, without secrets.
func (s *KeysService) List(ctx context.Context, opts ...RequestOption) (*List[APIKey], error) {
	var out List[APIKey]
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/keys"}, opts, &out)
	return orNil(&out, err)
}

// Delete revokes a key at once.
func (s *KeysService) Delete(ctx context.Context, id string, opts ...RequestOption) error {
	return s.c.doJSON(ctx, call{method: http.MethodDelete, path: "/v1/keys/" + seg(id)}, opts, nil)
}

// SuppressionsService lists and removes suppressed addresses.
type SuppressionsService struct{ c *Client }

// List returns the suppressed addresses.
func (s *SuppressionsService) List(ctx context.Context, opts ...RequestOption) (*List[Suppression], error) {
	var out List[Suppression]
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/suppressions"}, opts, &out)
	return orNil(&out, err)
}

// Delete lets mail go to the address again.
func (s *SuppressionsService) Delete(ctx context.Context, address string, opts ...RequestOption) error {
	return s.c.doJSON(ctx, call{method: http.MethodDelete, path: "/v1/suppressions/" + seg(address)}, opts, nil)
}

// EventsService reads the event log.
type EventsService struct{ c *Client }

// List returns events oldest first. Pass the returned NextAfter as After to continue.
func (s *EventsService) List(ctx context.Context, params EventsParams, opts ...RequestOption) (*EventPage, error) {
	q := url.Values{}
	if params.After != "" {
		q.Set("after", params.After)
	}
	if params.Limit > 0 {
		q.Set("limit", strconv.Itoa(params.Limit))
	}
	var out EventPage
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/events", query: q}, opts, &out)
	return orNil(&out, err)
}
