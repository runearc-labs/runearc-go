package runearc

import (
	"encoding/json"
	"time"
)

// List is a response with no pagination.
type List[T any] struct {
	Data []T `json:"data"`
}

// Page is a paginated response.
type Page[T any] struct {
	Data []T `json:"data"`
	// NextCursor is passed as Cursor to get the next page; nil on the last page.
	NextCursor *string `json:"next_cursor"`
}

// PageParams selects a page of a list.
type PageParams struct {
	// Limit is 1 to 100. The API default is 50.
	Limit int
	// Cursor is the NextCursor of the previous page.
	Cursor string
}

// Nullable is a value that can be sent as JSON null. Use a nil *Nullable to
// leave a field out, [Set] to send a value and [Clear] to send null.
type Nullable[T any] struct {
	value T
	null  bool
}

// Set sends v.
func Set[T any](v T) *Nullable[T] { return &Nullable[T]{value: v} }

// Clear sends JSON null, which unassigns or wakes a conversation.
func Clear[T any]() *Nullable[T] { return &Nullable[T]{null: true} }

// MarshalJSON implements [json.Marshaler].
func (n Nullable[T]) MarshalJSON() ([]byte, error) {
	if n.null {
		return []byte("null"), nil
	}
	return json.Marshal(n.value)
}

// ---------------------------------------------------------------------------
// Emails

// EmailAttachmentInput is an attachment of [SendEmailRequest].
type EmailAttachmentInput struct {
	Filename string `json:"filename,omitempty"`
	// Content is base64 (standard alphabet).
	Content string `json:"content,omitempty"`
	// ContentType is derived from the filename when empty.
	ContentType string `json:"content_type,omitempty"`
	// ContentID makes the attachment an inline image, referenced as cid:<content_id>.
	ContentID string `json:"content_id,omitempty"`
}

// Tag is a name and value attached to an email.
type Tag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// SendEmailRequest is the body of POST /v1/emails, shaped like Resend's.
type SendEmailRequest struct {
	// From is "Acme <noreply@acme.com>" or noreply@acme.com, on a verified domain.
	From        string                 `json:"from"`
	To          []string               `json:"to"`
	Cc          []string               `json:"cc,omitempty"`
	Bcc         []string               `json:"bcc,omitempty"`
	ReplyTo     []string               `json:"reply_to,omitempty"`
	Subject     string                 `json:"subject"`
	HTML        string                 `json:"html,omitempty"`
	Text        string                 `json:"text,omitempty"`
	Headers     map[string]string      `json:"headers,omitempty"`
	Attachments []EmailAttachmentInput `json:"attachments,omitempty"`
	Tags        []Tag                  `json:"tags,omitempty"`
	// ScheduledAt sends later: RFC 3339 with an offset (2026-10-20T09:00:00Z,
	// see time.RFC3339) or Resend's relative form ("in 1 hour"). At most 30 days ahead.
	ScheduledAt string `json:"scheduled_at,omitempty"`
}

// SendEmailResponse is the result of sending one email, and an item of a batch result.
type SendEmailResponse struct {
	ID string `json:"id"`
}

// Email statuses. The API may add more, so [Email].Status is a plain string.
const (
	EmailScheduled  = "scheduled"
	EmailQueued     = "queued"
	EmailSent       = "sent"
	EmailDelivered  = "delivered"
	EmailDeferred   = "deferred"
	EmailBounced    = "bounced"
	EmailFailed     = "failed"
	EmailComplained = "complained"
	EmailCanceled   = "canceled"
)

// Email is an email in a list.
type Email struct {
	ID        string   `json:"id"`
	ProjectID string   `json:"project_id"`
	DomainID  *string  `json:"domain_id"`
	From      string   `json:"from"`
	FromName  *string  `json:"from_name"`
	To        []string `json:"to"`
	Cc        []string `json:"cc"`
	Bcc       []string `json:"bcc"`
	ReplyTo   []string `json:"reply_to"`
	Subject   string   `json:"subject"`
	// Status is one of the Email* constants, or a newer status.
	Status    string `json:"status"`
	Tags      []Tag  `json:"tags"`
	SizeBytes int64  `json:"size_bytes"`
	// ScheduledAt is when the email is (or was) scheduled to send, or nil.
	ScheduledAt *time.Time `json:"scheduled_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// EmailRecipient is the delivery state of one recipient.
type EmailRecipient struct {
	Address string `json:"address"`
	// Kind is to, cc or bcc.
	Kind   string `json:"kind"`
	Status string `json:"status"`
	// Response is the receiving server's answer, such as "550 5.1.1 no such user".
	Response  *string   `json:"response"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EmailAttachment is an attachment of a sent email.
type EmailAttachment struct {
	ID          string  `json:"id"`
	Filename    string  `json:"filename"`
	ContentType string  `json:"content_type"`
	ContentID   *string `json:"content_id"`
	SizeBytes   int64   `json:"size_bytes"`
}

// EmailDetail is one email with its content and recipients.
type EmailDetail struct {
	Email
	Object string `json:"object"`
	// LastEvent is Resend's name for the latest status.
	LastEvent   string            `json:"last_event"`
	HTML        *string           `json:"html"`
	Text        *string           `json:"text"`
	Headers     map[string]string `json:"headers"`
	Attachments []EmailAttachment `json:"attachments"`
	Recipients  []EmailRecipient  `json:"recipients"`
}

// UpdateEmailRequest changes when a scheduled email sends.
type UpdateEmailRequest struct {
	// ScheduledAt is RFC 3339 with an offset, or a relative form such as "in 2 hours".
	ScheduledAt string `json:"scheduled_at"`
}

// EmailRef is the result of updating or canceling an email.
type EmailRef struct {
	Object string `json:"object"`
	ID     string `json:"id"`
}

// BatchItemError is one rejected item of a batch. Index is 0 based.
type BatchItemError struct {
	Index   int    `json:"index"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
}

// BatchResponse is the result of [EmailsService.Batch].
type BatchResponse struct {
	// Data has the ids of the emails accepted, in request order.
	Data []SendEmailResponse `json:"data"`
	// Errors is set only with [BatchPermissive]: the items that were refused.
	Errors []BatchItemError `json:"errors,omitempty"`
}

// SenderAddress is an address you can use as From.
type SenderAddress struct {
	// Address is noreply@acme.com, or *@acme.com for any address on a verified domain.
	Address  string `json:"address"`
	Domain   string `json:"domain"`
	DomainID string `json:"domain_id"`
	Verified bool   `json:"verified"`
	// Kind is sender or domain.
	Kind        string  `json:"kind"`
	DisplayName *string `json:"display_name"`
}

// ---------------------------------------------------------------------------
// Inboxes, messages, threads

// Inbox receives mail.
type Inbox struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	ProjectID   string    `json:"project_id"`
	Address     string    `json:"address"`
	LocalPart   string    `json:"local_part"`
	Domain      string    `json:"domain"`
	DisplayName *string   `json:"display_name"`
	CatchAll    bool      `json:"catch_all"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateInboxRequest is the body of POST /v1/inboxes. Every field is optional.
type CreateInboxRequest struct {
	// LocalPart is random when empty.
	LocalPart   string `json:"local_part,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	// Domain is a custom domain of the workspace; the shared domain when empty.
	Domain string `json:"domain,omitempty"`
}

// MessageAttachment is an attachment of a received or sent message.
type MessageAttachment struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	MessageID   string    `json:"message_id"`
	Filename    *string   `json:"filename"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	CreatedAt   time.Time `json:"created_at"`
}

// Message is an inbound or outbound message of an inbox.
type Message struct {
	ID        string `json:"id"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	InboxID   string `json:"inbox_id"`
	ThreadID  string `json:"thread_id"`
	// Direction is inbound or outbound.
	Direction    string              `json:"direction"`
	RFCMessageID *string             `json:"rfc_message_id"`
	InReplyTo    *string             `json:"in_reply_to"`
	References   []string            `json:"references"`
	From         string              `json:"from"`
	To           []string            `json:"to"`
	Cc           []string            `json:"cc"`
	DeliveredTo  []string            `json:"delivered_to"`
	Subject      string              `json:"subject"`
	Text         *string             `json:"text"`
	HTML         *string             `json:"html"`
	SizeBytes    int64               `json:"size_bytes"`
	Status       string              `json:"status"`
	Spam         bool                `json:"spam"`
	AuthResults  json.RawMessage     `json:"auth_results"`
	CreatedAt    time.Time           `json:"created_at"`
	Attachments  []MessageAttachment `json:"attachments"`
}

// AttachmentInput is an attachment of [SendMessageRequest] or [ReplyRequest].
type AttachmentInput struct {
	Filename string `json:"filename"`
	// ContentType defaults to application/octet-stream.
	ContentType string `json:"content_type,omitempty"`
	// Content is base64 (standard alphabet).
	Content string `json:"content"`
}

// SendMessageRequest sends a new message from an inbox.
type SendMessageRequest struct {
	To          []string          `json:"to"`
	Cc          []string          `json:"cc,omitempty"`
	Bcc         []string          `json:"bcc,omitempty"`
	Subject     string            `json:"subject"`
	Text        string            `json:"text,omitempty"`
	HTML        string            `json:"html,omitempty"`
	ReplyTo     []string          `json:"reply_to,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Attachments []AttachmentInput `json:"attachments,omitempty"`
}

// ReplyRequest replies in the thread of a message.
type ReplyRequest struct {
	Text string `json:"text,omitempty"`
	HTML string `json:"html,omitempty"`
	// ReplyAll also copies everyone else on the original message.
	ReplyAll    bool              `json:"reply_all,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Attachments []AttachmentInput `json:"attachments,omitempty"`
}

// WaitParams selects which message [MessagesService.Wait] waits for.
type WaitParams struct {
	// From is a case insensitive substring of the sender address, such as "github.com".
	From string
	// Subject is a case insensitive substring of the subject.
	Subject string
	// To is a recipient address, matched exactly.
	To string
	// Timeout is how long to wait, 1 to 120 seconds. Default 30.
	Timeout time.Duration
	// After accepts only messages received after this time. The API default is
	// when the wait starts.
	After time.Time
}

// Waited is a message that [MessagesService.Wait] found.
type Waited struct {
	Message Message `json:"message"`
	// Codes are the one time codes found in the subject and body.
	Codes []string `json:"codes"`
	Links []string `json:"links"`
}

// Thread is a conversation of messages in one inbox.
type Thread struct {
	ID            string    `json:"id"`
	OrgID         string    `json:"org_id"`
	ProjectID     string    `json:"project_id"`
	InboxID       string    `json:"inbox_id"`
	Subject       string    `json:"subject"`
	LastMessageAt time.Time `json:"last_message_at"`
	MessageCount  int       `json:"message_count"`
	CreatedAt     time.Time `json:"created_at"`
	// Status is open or closed.
	Status         string     `json:"status"`
	AssigneeUserID *string    `json:"assignee_user_id"`
	SnoozedUntil   *time.Time `json:"snoozed_until"`
	LastInboundAt  *time.Time `json:"last_inbound_at"`
}

// ThreadWithMessages is a thread with its messages.
type ThreadWithMessages struct {
	Thread
	Messages []Message `json:"messages"`
}

// ---------------------------------------------------------------------------
// Conversations

// ConversationProject names the project of a conversation.
type ConversationProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ConversationInbox names the inbox of a conversation.
type ConversationInbox struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

// ConversationAssignee is the user a conversation is assigned to.
type ConversationAssignee struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// Conversation is a thread in the shared support view.
type Conversation struct {
	// ID is the thread id.
	ID      string              `json:"id"`
	Project ConversationProject `json:"project"`
	Inbox   ConversationInbox   `json:"inbox"`
	Subject string              `json:"subject"`
	Snippet string              `json:"snippet"`
	// Status is open or closed.
	Status        string                `json:"status"`
	SnoozedUntil  *time.Time            `json:"snoozed_until"`
	Assignee      *ConversationAssignee `json:"assignee"`
	Unread        bool                  `json:"unread"`
	MessageCount  int                   `json:"message_count"`
	LastMessageAt time.Time             `json:"last_message_at"`
	LastInboundAt *time.Time            `json:"last_inbound_at"`
	LastFrom      string                `json:"last_from"`
}

// ConversationFilter selects conversations. Empty fields are not sent.
type ConversationFilter struct {
	// Status is open, closed, snoozed or all.
	Status string
	// Assignee is "me", "none" or a user id.
	Assignee  string
	ProjectID string
	InboxID   string
	// Domain is a comma separated list of domain names.
	Domain string
	// Q searches subjects and snippets.
	Q      string
	Cursor string
	Limit  int
}

// ConversationUpdate changes a conversation. A nil field is left alone.
type ConversationUpdate struct {
	// Status is open or closed.
	Status string `json:"status,omitempty"`
	// AssigneeUserID assigns with [Set] and unassigns with [Clear].
	AssigneeUserID *Nullable[string] `json:"assignee_user_id,omitempty"`
	// SnoozedUntil snoozes with [Set] and wakes the conversation with [Clear].
	SnoozedUntil *Nullable[time.Time] `json:"snoozed_until,omitempty"`
}

// ProjectCount is the open and unread conversations of a project.
type ProjectCount struct {
	ProjectID string `json:"project_id"`
	Open      int    `json:"open"`
	Unread    int    `json:"unread"`
}

// InboxCount is the open and unread conversations of an inbox.
type InboxCount struct {
	InboxID   string `json:"inbox_id"`
	ProjectID string `json:"project_id"`
	Open      int    `json:"open"`
	Unread    int    `json:"unread"`
}

// ConversationCounts is the result of [ConversationsService.Counts].
type ConversationCounts struct {
	Open         int            `json:"open"`
	AssignedToMe int            `json:"assigned_to_me"`
	Unassigned   int            `json:"unassigned"`
	ByProject    []ProjectCount `json:"by_project"`
	ByInbox      []InboxCount   `json:"by_inbox"`
}

// ---------------------------------------------------------------------------
// Domains

// DomainRecord is a DNS record to publish for a domain.
type DomainRecord struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	Priority *int   `json:"priority"`
	Required bool   `json:"required"`
	// Status is unchecked, ok, missing or wrong.
	Status        string     `json:"status"`
	Fix           *string    `json:"fix"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
}

// Domain is a sending and receiving domain.
type Domain struct {
	ID             string  `json:"id"`
	OrgID          string  `json:"org_id"`
	ProjectID      string  `json:"project_id"`
	Name           string  `json:"name"`
	ParentDomainID *string `json:"parent_domain_id"`
	// Status is pending, verified or failing.
	Status           string         `json:"status"`
	Provider         *string        `json:"provider"`
	CanAutoConfigure bool           `json:"can_auto_configure"`
	DKIMSelector     string         `json:"dkim_selector"`
	VerifiedAt       *time.Time     `json:"verified_at"`
	LastCheckedAt    *time.Time     `json:"last_checked_at"`
	CreatedAt        time.Time      `json:"created_at"`
	Records          []DomainRecord `json:"records"`
	// NextStep is the one thing to do next.
	NextStep string `json:"next_step"`
}

// ExistingMX describes mail records the domain already has.
type ExistingMX struct {
	Hosts      []string `json:"hosts"`
	Suggestion string   `json:"suggestion"`
	Message    string   `json:"message"`
}

// AddedDomain is the result of [DomainsService.Create].
type AddedDomain struct {
	Domain
	ExistingMX *ExistingMX `json:"existing_mx"`
}

// ---------------------------------------------------------------------------
// Webhooks and events

// Webhook is a webhook endpoint.
type Webhook struct {
	ID        string `json:"id"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	URL       string `json:"url"`
	// EventTypes is empty for every event type.
	EventTypes []string `json:"event_types"`
	// InboxID is nil for every inbox in the project.
	InboxID   *string   `json:"inbox_id"`
	CreatedAt time.Time `json:"created_at"`
}

// CreatedWebhook is the result of [WebhooksService.Create].
type CreatedWebhook struct {
	Webhook
	// Secret is whsec_..., shown once: store it to verify signatures.
	Secret string `json:"secret"`
}

// CreateWebhookRequest is the body of POST /v1/webhooks.
type CreateWebhookRequest struct {
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types,omitempty"`
	// InboxID limits the webhook to one inbox. Empty means every inbox.
	InboxID string `json:"inbox_id,omitempty"`
}

// Delivery is one attempt to deliver an event to a webhook.
type Delivery struct {
	ID                  string     `json:"id"`
	WebhookID           string     `json:"webhook_id"`
	EventID             string     `json:"event_id"`
	ReplayOf            *string    `json:"replay_of"`
	Attempt             int        `json:"attempt"`
	Status              string     `json:"status"`
	ResponseCode        *int       `json:"response_code"`
	ResponseBodySnippet *string    `json:"response_body_snippet"`
	NextAttemptAt       *time.Time `json:"next_attempt_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// Event is an entry of the event log. Data is the event's JSON payload.
type Event struct {
	ID        string          `json:"id"`
	OrgID     string          `json:"org_id"`
	ProjectID string          `json:"project_id"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

// EventsParams selects events of [EventsService.List].
type EventsParams struct {
	// After is the NextAfter of the previous page.
	After string
	Limit int
}

// EventPage is a page of events, oldest first.
type EventPage struct {
	Data []Event `json:"data"`
	// NextAfter is passed as After to get the next events.
	NextAfter *string `json:"next_after"`
}

// WebhookEvent is the JSON body Runearc posts to a webhook endpoint. Decode
// Data into your own type.
type WebhookEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// ---------------------------------------------------------------------------
// Keys and suppressions

// APIKey is an API key without its secret.
type APIKey struct {
	ID        string `json:"id"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Prefix    string `json:"prefix"`
	Scope     string `json:"scope"`
	// Permissions is full, sending or inbox.
	Permissions string     `json:"permissions"`
	DomainID    *string    `json:"domain_id"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// CreateKeyRequest is the body of POST /v1/keys.
type CreateKeyRequest struct {
	Name string `json:"name"`
	// Permissions is full (the default), sending or inbox.
	Permissions string `json:"permissions,omitempty"`
	// InboxID is the one inbox of an inbox key.
	InboxID string `json:"inbox_id,omitempty"`
	// DomainID limits a sending key to one domain (id or name).
	DomainID string `json:"domain_id,omitempty"`
}

// CreatedKey is the result of [KeysService.Create].
type CreatedKey struct {
	Key APIKey `json:"key"`
	// Secret is shown once; Runearc stores only a hash.
	Secret string `json:"secret"`
}

// Suppression is an address that mail is not sent to.
type Suppression struct {
	Address   string    `json:"address"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}
