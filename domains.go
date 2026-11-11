package runearc

import (
	"context"
	"net/http"
)

// DomainsService manages sending and receiving domains.
type DomainsService struct{ c *Client }

// Create adds a domain and returns the DNS records to publish.
func (s *DomainsService) Create(ctx context.Context, name string, opts ...RequestOption) (*AddedDomain, error) {
	var out AddedDomain
	body := struct {
		Name string `json:"name"`
	}{name}
	err := s.c.doJSON(ctx, call{method: http.MethodPost, path: "/v1/domains", body: body}, opts, &out)
	return orNil(&out, err)
}

// List returns every domain.
func (s *DomainsService) List(ctx context.Context, opts ...RequestOption) (*List[Domain], error) {
	var out List[Domain]
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/domains"}, opts, &out)
	return orNil(&out, err)
}

// Get returns one domain. id is a domain id (dom_...) or the domain name.
func (s *DomainsService) Get(ctx context.Context, id string, opts ...RequestOption) (*Domain, error) {
	var out Domain
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/domains/" + seg(id)}, opts, &out)
	return orNil(&out, err)
}

// Delete deletes a domain. id is a domain id or the domain name.
func (s *DomainsService) Delete(ctx context.Context, id string, opts ...RequestOption) error {
	return s.c.doJSON(ctx, call{method: http.MethodDelete, path: "/v1/domains/" + seg(id)}, opts, nil)
}

// Verify checks the DNS records now and returns the domain with each record's status.
func (s *DomainsService) Verify(ctx context.Context, id string, opts ...RequestOption) (*Domain, error) {
	var out Domain
	err := s.c.doJSON(ctx, call{method: http.MethodPost, path: "/v1/domains/" + seg(id) + "/verify", idempotent: true}, opts, &out)
	return orNil(&out, err)
}
