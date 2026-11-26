package cli

import (
	"fmt"
	"mime"
	"sort"
	"strings"
	"time"

	"github.com/bscott/pm-cli/internal/imap"
)

type MailReadEvidenceCmd struct {
	MessageID string `arg:"" name:"message-id" help:"RFC Message-ID from mail list"`
	Mailbox   string `help:"Mailbox name" default:"INBOX"`
	Headers   bool   `help:"Include full message headers"`
}

type MailEvidenceExistsCmd struct {
	MessageID string `arg:"" name:"message-id" help:"RFC Message-ID to find"`
	Mailbox   string `help:"Mailbox name" default:"INBOX"`
}

type MailListEvidenceCmd struct {
	Mailbox     string `help:"Mailbox name" default:"INBOX"`
	Limit       int    `help:"Maximum messages on one page (1–10000)" default:"20"`
	Offset      int    `help:"Skip messages after filtering" default:"0"`
	Unread      bool   `help:"Only include unread messages"`
	IncludeBody bool   `help:"Include body and headers in the same read-only session"`
	SentSince   string `help:"Inclusive message Date lower bound (YYYY-MM-DD)"`
	SentBefore  string `help:"Exclusive message Date upper bound (YYYY-MM-DD)"`
}

type MailDeleteEvidenceCmd struct {
	MessageID string `arg:"" name:"message-id" help:"RFC Message-ID to permanently delete"`
	Mailbox   string `help:"Mailbox name" default:"INBOX"`
}

type MailboxListEvidenceCmd struct{}

func (c *MailboxListEvidenceCmd) Run(ctx *Context) error {
	return withEvidenceClient(ctx, func(client *imap.Client) error {
		items, err := client.ListMailboxes()
		if err != nil {
			return err
		}
		mailboxes := make([]map[string]string, 0, len(items))
		for _, item := range items {
			mailboxes = append(mailboxes, map[string]string{"name": item.Name, "mailbox": item.Name})
		}
		return ctx.Formatter.PrintJSON(map[string]any{"mailboxes": mailboxes})
	})
}

type evidenceHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type bodyEvidence struct {
	Body     string           `json:"body"`
	HTMLBody string           `json:"html_body,omitempty"`
	Headers  []evidenceHeader `json:"headers,omitempty"`
}

type messageEvidence struct {
	MessageID *string `json:"message_id"`
	Sender    *string `json:"sender"`
	Subject   *string `json:"subject"`
	Date      *string `json:"date"`
	Seen      bool    `json:"seen"`
	*bodyEvidence
}

func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func decodeHeader(value string) string {
	decoded, err := new(mime.WordDecoder).DecodeHeader(value)
	if err != nil {
		return value
	}
	return decoded
}

func evidenceWithBody(record imap.Evidence, includeHeaders bool) bodyEvidence {
	body, html := parseMessageBody(record.HeadersAndBody)
	if strings.TrimSpace(body) != "" {
		html = ""
	}
	output := bodyEvidence{Body: body, HTMLBody: html}
	if includeHeaders {
		names := make([]string, 0, len(record.Headers))
		for name := range record.Headers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			for _, value := range record.Headers[name] {
				output.Headers = append(output.Headers, evidenceHeader{name, decodeHeader(value)})
			}
		}
	}
	return output
}

func withEvidenceClient(ctx *Context, operation func(*imap.Client) error) error {
	client, err := imap.NewClient(ctx.Config)
	if err != nil {
		return err
	}
	if err := client.Connect(); err != nil {
		return err
	}
	defer client.Close()
	return operation(client)
}

func (c *MailReadEvidenceCmd) Run(ctx *Context) error {
	return withEvidenceClient(ctx, func(client *imap.Client) error {
		record, err := client.ReadEvidence(c.Mailbox, c.MessageID)
		if err != nil {
			return err
		}
		if record.Headers.Get("Message-ID") != c.MessageID {
			return fmt.Errorf("message identity changed during read")
		}
		return ctx.Formatter.PrintJSON(struct {
			MessageID string `json:"message_id"`
			bodyEvidence
		}{c.MessageID, evidenceWithBody(record, c.Headers)})
	})
}

func (c *MailEvidenceExistsCmd) Run(ctx *Context) error {
	return withEvidenceClient(ctx, func(client *imap.Client) error {
		exists, err := client.EvidenceExists(c.Mailbox, c.MessageID)
		if err != nil {
			return err
		}
		return ctx.Formatter.PrintJSON(map[string]bool{"exists": exists})
	})
}

func (c *MailListEvidenceCmd) Run(ctx *Context) error {
	date := func(value string) (time.Time, error) {
		if value == "" {
			return time.Time{}, nil
		}
		return time.Parse(time.DateOnly, value)
	}
	since, err := date(c.SentSince)
	if err != nil {
		return err
	}
	before, err := date(c.SentBefore)
	if err != nil {
		return err
	}
	return withEvidenceClient(ctx, func(client *imap.Client) error {
		rows := make([]messageEvidence, 0)
		err := client.WalkEvidence(imap.EvidenceOptions{
			Mailbox: c.Mailbox, Limit: c.Limit, Offset: c.Offset, Unread: c.Unread,
			FullBody: c.IncludeBody, SentSince: since, SentBefore: before,
		}, func(record imap.Evidence) error {
			row := messageEvidence{
				MessageID: nullable(record.Headers.Get("Message-ID")),
				Sender:    nullable(decodeHeader(record.Headers.Get("From"))),
				Subject:   nullable(decodeHeader(record.Headers.Get("Subject"))),
				Date:      nullable(record.Headers.Get("Date")), Seen: record.Seen,
			}
			if c.IncludeBody {
				body := evidenceWithBody(record, true)
				row.bodyEvidence = &body
			}
			rows = append(rows, row)
			return nil
		})
		if err != nil {
			return err
		}
		return ctx.Formatter.PrintJSON(map[string]any{"messages": rows})
	})
}

func (c *MailDeleteEvidenceCmd) Run(ctx *Context) error {
	return withEvidenceClient(ctx, func(client *imap.Client) error {
		if err := client.ExpungeEvidence(c.Mailbox, c.MessageID); err != nil {
			return err
		}
		return ctx.Formatter.PrintJSON(map[string]string{"message_id": c.MessageID})
	})
}
