package notify

import (
	"context"
	"fmt"
	"strings"

	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/router"
	"github.com/containrrr/shoutrrr/pkg/types"
)

// Sender sends notifications to one or more Shoutrrr endpoints.
type Sender struct {
	sender *router.ServiceRouter
	urls   []string
}

// New creates a notification sender from a comma-separated or newline-separated
// list of Shoutrrr URLs.
func New(raw string) (*Sender, error) {
	urls := splitURLs(raw)
	if len(urls) == 0 {
		return nil, nil
	}

	sender, err := shoutrrr.CreateSender(urls...)
	if err != nil {
		return nil, fmt.Errorf("creating notification sender: %w", err)
	}

	return &Sender{sender: sender, urls: urls}, nil
}

// Enabled reports whether notifications are configured.
func (s *Sender) Enabled() bool {
	return s != nil && len(s.urls) > 0
}

// Send dispatches a message to all configured destinations.
func (s *Sender) Send(_ context.Context, message string, params map[string]string) error {
	if !s.Enabled() {
		return nil
	}

	shoutrrrParams := types.Params{}
	for key, value := range params {
		shoutrrrParams[key] = value
	}

	errList := s.sender.Send(message, &shoutrrrParams)
	if len(errList) == 0 {
		return nil
	}

	parts := make([]string, 0, len(errList))
	for _, err := range errList {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	if len(parts) == 0 {
		return nil
	}

	return fmt.Errorf("sending notification: %s", strings.Join(parts, "; "))
}

func splitURLs(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n'
	})

	urls := make([]string, 0, len(fields))
	for _, field := range fields {
		url := strings.TrimSpace(field)
		if url != "" {
			urls = append(urls, url)
		}
	}

	return urls
}
