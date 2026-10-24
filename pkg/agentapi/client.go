// Package agentapi is a client for the AgentAPI managed-agent sessions,
// events and files APIs. It is a thin adapter over the official Anthropic Go
// SDK, which AgentAPI implements.
package agentapi

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
)

// Session is the AgentAPI session snapshot used by the builder.
type Session struct {
	ID         string      `json:"id"`
	Status     string      `json:"status"`
	StopReason *StopReason `json:"stop_reason,omitempty"`
}

// StopReason explains why a session round stopped.
type StopReason struct {
	Type     string   `json:"type"`
	EventIDs []string `json:"event_ids,omitempty"`
}

// Content is one block of an agent message.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Event is one Server-Sent Event from the session stream.
type Event struct {
	ID         string      `json:"id"`
	Type       string      `json:"type"`
	Content    []Content   `json:"content,omitempty"`
	StopReason *StopReason `json:"stop_reason,omitempty"`
	Message    string      `json:"message,omitempty"`
	Error      *EventError `json:"error,omitempty"`
}

// EventError is the error payload of a session.error event.
type EventError struct {
	Message string `json:"message"`
}

// Text returns the concatenated text blocks of an agent.message event.
func (e Event) Text() string {
	var builder strings.Builder
	for _, block := range e.Content {
		if block.Type == "text" {
			builder.WriteString(block.Text)
		}
	}
	return builder.String()
}

// MessageText returns the most useful human-readable error text.
func (e Event) MessageText() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Error != nil {
		return e.Error.Message
	}
	return ""
}

// File is one AgentAPI file object.
type File struct {
	ID           string    `json:"id"`
	Filename     string    `json:"filename"`
	MimeType     string    `json:"mime_type"`
	SizeBytes    int64     `json:"size_bytes"`
	Downloadable bool      `json:"downloadable"`
	CreatedAt    time.Time `json:"created_at"`
}

// Client calls AgentAPI.
type Client struct {
	api     anthropic.Client
	timeout time.Duration
}

// New creates a client. timeout applies to ordinary requests; streaming and
// downloads use the caller's context deadline.
func New(baseURL, apiKey string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		api: anthropic.NewClient(
			option.WithoutEnvironmentDefaults(),
			option.WithBaseURL(strings.TrimRight(baseURL, "/")),
			option.WithAPIKey(apiKey),
			option.WithMaxRetries(0),
		),
		timeout: timeout,
	}
}

func (c *Client) options() []option.RequestOption {
	if c.timeout <= 0 {
		return nil
	}
	return []option.RequestOption{option.WithRequestTimeout(c.timeout)}
}

// CreateSession creates an idle build session pinned to the agent version.
func (c *Client) CreateSession(ctx context.Context, agentID string, agentVersion int, environmentID, title string, metadata map[string]string) (Session, error) {
	agent := &anthropic.BetaManagedAgentsAgentParams{
		ID:   agentID,
		Type: anthropic.BetaManagedAgentsAgentParamsTypeAgent,
	}
	if agentVersion > 0 {
		agent.Version = param.NewOpt(int64(agentVersion))
	}
	params := anthropic.BetaSessionNewParams{
		Agent: anthropic.BetaSessionNewParamsAgentUnion{
			OfBetaManagedAgentsAgents: agent,
		},
		EnvironmentID: environmentID,
	}
	if title != "" {
		params.Title = param.NewOpt(title)
	}
	if len(metadata) > 0 {
		params.Metadata = metadata
	}
	session, err := c.api.Beta.Sessions.New(ctx, params, c.options()...)
	if err != nil {
		return Session{}, err
	}
	return convertSession(session), nil
}

// SubmitMessage sends one user.message event.
func (c *Client) SubmitMessage(ctx context.Context, sessionID, text string) error {
	block := anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{
		OfText: &anthropic.BetaManagedAgentsTextBlockParam{
			Type: anthropic.BetaManagedAgentsTextBlockTypeText,
			Text: text,
		},
	}
	_, err := c.api.Beta.Sessions.Events.Send(ctx, sessionID, anthropic.BetaSessionEventSendParams{
		Events: []anthropic.BetaManagedAgentsEventParamsUnion{
			anthropic.BetaManagedAgentsEventParamsOfUserMessage(
				[]anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{block},
			),
		},
	}, c.options()...)
	return err
}

// Interrupt sends a user.interrupt event.
func (c *Client) Interrupt(ctx context.Context, sessionID string) error {
	_, err := c.api.Beta.Sessions.Events.Send(ctx, sessionID, anthropic.BetaSessionEventSendParams{
		Events: []anthropic.BetaManagedAgentsEventParamsUnion{
			anthropic.BetaManagedAgentsEventParamsOfUserInterrupt(anthropic.BetaManagedAgentsUserInterruptEventParamsTypeUserInterrupt),
		},
	}, c.options()...)
	return err
}

// GetSession returns the current session snapshot.
func (c *Client) GetSession(ctx context.Context, sessionID string) (Session, error) {
	session, err := c.api.Beta.Sessions.Get(ctx, sessionID, anthropic.BetaSessionGetParams{}, c.options()...)
	if err != nil {
		return Session{}, err
	}
	return convertSession(session), nil
}

// ArchiveSession archives a finished build session.
func (c *Client) ArchiveSession(ctx context.Context, sessionID string) error {
	_, err := c.api.Beta.Sessions.Archive(ctx, sessionID, anthropic.BetaSessionArchiveParams{}, c.options()...)
	return err
}

// ListFiles returns every file in the session scope, following next_page.
func (c *Client) ListFiles(ctx context.Context, sessionID string) ([]File, error) {
	pager := c.api.Beta.Files.ListAutoPaging(ctx, anthropic.BetaFileListParams{
		ScopeID: param.NewOpt(sessionID),
		Limit:   param.NewOpt(int64(100)),
	}, c.options()...)
	var files []File
	for pager.Next() {
		item := pager.Current()
		files = append(files, File{
			ID:           item.ID,
			Filename:     item.Filename,
			MimeType:     item.MimeType,
			SizeBytes:    item.SizeBytes,
			Downloadable: item.Downloadable,
			CreatedAt:    item.CreatedAt,
		})
	}
	if err := pager.Err(); err != nil {
		return nil, err
	}
	return files, nil
}

// DownloadFile streams one file, failing if it exceeds limit bytes.
func (c *Client) DownloadFile(ctx context.Context, fileID string, dst io.Writer, limit int64) (int64, error) {
	response, err := c.api.Beta.Files.Download(ctx, fileID, anthropic.BetaFileDownloadParams{})
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	written, err := io.Copy(dst, io.LimitReader(response.Body, limit+1))
	if err != nil {
		return written, err
	}
	if written > limit {
		return written, fmt.Errorf("agentapi: file exceeds %d bytes", limit)
	}
	return written, nil
}

// EventStream reads one SSE connection.
type EventStream struct {
	stream *ssestream.Stream[anthropic.BetaManagedAgentsStreamSessionEventsUnion]
}

// OpenEventStream subscribes to a session's event stream.
func (c *Client) OpenEventStream(ctx context.Context, sessionID string) (*EventStream, error) {
	stream := c.api.Beta.Sessions.Events.StreamEvents(ctx, sessionID, anthropic.BetaSessionEventStreamParams{}, option.WithHeader("Accept", "text/event-stream"))
	return &EventStream{stream: stream}, nil
}

// Next returns the next event, or io.EOF when the connection ends.
func (s *EventStream) Next() (Event, error) {
	if s.stream.Next() {
		return convertStreamEvent(s.stream.Current()), nil
	}
	if err := s.stream.Err(); err != nil {
		return Event{}, err
	}
	return Event{}, io.EOF
}

// Close closes the SSE connection.
func (s *EventStream) Close() error { return s.stream.Close() }

func convertSession(session *anthropic.BetaManagedAgentsSession) Session {
	if session == nil {
		return Session{}
	}
	return Session{ID: session.ID, Status: string(session.Status)}
}

func convertStreamEvent(union anthropic.BetaManagedAgentsStreamSessionEventsUnion) Event {
	event := Event{ID: union.ID, Type: union.Type}
	for _, block := range union.Content.OfBetaManagedAgentsAgentMessageEventContentArray {
		event.Content = append(event.Content, Content{Type: block.Type, Text: block.Text})
	}
	if union.StopReason.Type != "" {
		event.StopReason = &StopReason{
			Type:     union.StopReason.Type,
			EventIDs: union.StopReason.EventIDs,
		}
	}
	if union.Error.Message != "" {
		event.Message = union.Error.Message
		event.Error = &EventError{Message: union.Error.Message}
	}
	return event
}
