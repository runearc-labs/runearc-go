package runearc

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

func (p PageParams) values() url.Values {
	v := url.Values{}
	if p.Limit > 0 {
		v.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Cursor != "" {
		v.Set("cursor", p.Cursor)
	}
	return v
}

// EmailsService sends, schedules and reads email.
type EmailsService struct{ c *Client }

// Send sends one email, or schedules it when req.ScheduledAt is set. It gets an
// Idempotency-Key automatically unless [WithIdempotencyKey] gives one.
func (s *EmailsService) Send(ctx context.Context, req *SendEmailRequest, opts ...RequestOption) (*SendEmailResponse, error) {
	var out SendEmailResponse
	err := s.c.doJSON(ctx, call{method: http.MethodPost, path: "/v1/emails", body: req, sendsMail: true}, opts, &out)
	return orNil(&out, err)
}

// Batch sends up to 100 emails in one request. By default one invalid item
// refuses the whole batch with the code invalid_batch; see [WithBatchValidation].
func (s *EmailsService) Batch(ctx context.Context, emails []SendEmailRequest, opts ...RequestOption) (*BatchResponse, error) {
	if emails == nil {
		emails = []SendEmailRequest{}
	}
	var out BatchResponse
	err := s.c.doJSON(ctx, call{method: http.MethodPost, path: "/v1/emails/batch", body: emails, sendsMail: true}, opts, &out)
	return orNil(&out, err)
}

// Get returns one email with its content and recipients.
func (s *EmailsService) Get(ctx context.Context, id string, opts ...RequestOption) (*EmailDetail, error) {
	var out EmailDetail
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/emails/" + seg(id)}, opts, &out)
	return orNil(&out, err)
}

// List returns a page of emails, newest first.
func (s *EmailsService) List(ctx context.Context, params PageParams, opts ...RequestOption) (*Page[Email], error) {
	var out Page[Email]
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/emails", query: params.values()}, opts, &out)
	return orNil(&out, err)
}

// Update changes when a scheduled email sends.
func (s *EmailsService) Update(ctx context.Context, id string, req UpdateEmailRequest, opts ...RequestOption) (*EmailRef, error) {
	var out EmailRef
	err := s.c.doJSON(ctx, call{method: http.MethodPatch, path: "/v1/emails/" + seg(id), body: req}, opts, &out)
	return orNil(&out, err)
}

// Reschedule is shorthand for [EmailsService.Update] with a new ScheduledAt.
func (s *EmailsService) Reschedule(ctx context.Context, id, scheduledAt string, opts ...RequestOption) (*EmailRef, error) {
	return s.Update(ctx, id, UpdateEmailRequest{ScheduledAt: scheduledAt}, opts...)
}

// Cancel cancels a scheduled email.
func (s *EmailsService) Cancel(ctx context.Context, id string, opts ...RequestOption) (*EmailRef, error) {
	var out EmailRef
	err := s.c.doJSON(ctx, call{method: http.MethodPost, path: "/v1/emails/" + seg(id) + "/cancel", idempotent: true}, opts, &out)
	return orNil(&out, err)
}

// SendersService lists the addresses you can send from.
type SendersService struct{ c *Client }

// List returns the addresses you can use as From.
func (s *SendersService) List(ctx context.Context, opts ...RequestOption) (*List[SenderAddress], error) {
	var out List[SenderAddress]
	err := s.c.doJSON(ctx, call{method: http.MethodGet, path: "/v1/senders"}, opts, &out)
	return orNil(&out, err)
}

// orNil returns v, or nil when the call failed.
func orNil[T any](v *T, err error) (*T, error) {
	if err != nil {
		return nil, err
	}
	return v, nil
}
