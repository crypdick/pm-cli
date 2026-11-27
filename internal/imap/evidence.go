package imap

import (
	"bytes"
	"errors"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
)

var ErrEvidenceNotFound = errors.New("Message-ID was not found in the selected mailbox")

// EvidenceOptions bounds a read-only mailbox page by the message Date header,
// not the date the server received or imported the message.
type EvidenceOptions struct {
	Mailbox    string
	Limit      int
	Offset     int
	Unread     bool
	FullBody   bool
	SentSince  time.Time
	SentBefore time.Time
}

type Evidence struct {
	Headers        mail.Header
	HeadersAndBody []byte
	Seen           bool
}

func (c *Client) WalkEvidence(options EvidenceOptions, visit func(Evidence) error) error {
	if options.Limit < 1 || options.Limit > 10000 || options.Offset < 0 {
		return fmt.Errorf("limit must be 1–10000 and offset must be nonnegative")
	}
	if !options.SentSince.IsZero() && !options.SentBefore.IsZero() && !options.SentBefore.After(options.SentSince) {
		return fmt.Errorf("sent-before must be after sent-since")
	}
	if _, err := c.client.Select(options.Mailbox, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return err
	}
	criteria := &imap.SearchCriteria{SentSince: options.SentSince, SentBefore: options.SentBefore}
	if options.Unread {
		criteria.NotFlag = []imap.Flag{imap.FlagSeen}
	}
	search, err := c.client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return err
	}
	uids := search.AllUIDs()
	slices.Sort(uids)
	slices.Reverse(uids)
	start := min(options.Offset, len(uids))
	uids = uids[start : start+min(options.Limit, len(uids)-start)]
	for _, uid := range uids {
		item, err := c.fetchEvidence(uid, options.FullBody)
		if err != nil {
			return err
		}
		if err := visit(item); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) ReadEvidence(mailbox, messageID string) (Evidence, error) {
	uid, err := c.findEvidenceUID(mailbox, messageID, true)
	if err != nil {
		return Evidence{}, err
	}
	return c.fetchEvidence(uid, true)
}

func (c *Client) EvidenceExists(mailbox, messageID string) (bool, error) {
	_, err := c.findEvidenceUID(mailbox, messageID, true)
	if errors.Is(err, ErrEvidenceNotFound) {
		return false, nil
	}
	return err == nil, err
}

// ExpungeEvidence removes only the exact message; never issue a mailbox-wide
// EXPUNGE or mark anything deleted on servers without UID-scoped expunge.
func (c *Client) ExpungeEvidence(mailbox, messageID string) error {
	if !c.client.Caps().Has(imap.CapUIDPlus) {
		return fmt.Errorf("IMAP server does not support UID-scoped expunge")
	}
	uid, err := c.findEvidenceUID(mailbox, messageID, false)
	if err != nil {
		return err
	}
	uids := imap.UIDSetNum(uid)
	changed, err := c.client.Store(uids, &imap.StoreFlags{
		Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted},
	}, nil).Collect()
	if err != nil {
		return err
	}
	if len(changed) != 1 {
		return fmt.Errorf("message disappeared before permanent deletion")
	}
	removed, err := c.client.UIDExpunge(uids).Collect()
	if err != nil {
		return err
	}
	if len(removed) != 1 {
		return fmt.Errorf("IMAP did not confirm targeted expunge")
	}
	return nil
}

func (c *Client) findEvidenceUID(mailbox, messageID string, readOnly bool) (imap.UID, error) {
	if messageID == "" || strings.ContainsAny(messageID, "\r\n\x00") || strings.TrimSpace(messageID) != messageID {
		return 0, fmt.Errorf("expected one RFC Message-ID")
	}
	if _, err := c.client.Select(mailbox, &imap.SelectOptions{ReadOnly: readOnly}).Wait(); err != nil {
		return 0, err
	}
	search, err := c.client.UIDSearch(&imap.SearchCriteria{
		Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: messageID}},
	}, nil).Wait()
	if err != nil {
		return 0, err
	}
	// IMAP HEADER matches substrings. Verify the actual header before using a UID.
	var match imap.UID
	for _, uid := range search.AllUIDs() {
		section := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, HeaderFields: []string{"Message-ID"}, Peek: true}
		data, err := c.client.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{BodySection: []*imap.FetchItemBodySection{section}}).Collect()
		if err != nil {
			return 0, err
		}
		if len(data) == 0 {
			continue
		}
		header, err := evidenceHeaders(data[0].FindBodySection(section))
		if err != nil {
			return 0, err
		}
		values := header["Message-Id"]
		if len(values) != 1 || values[0] != messageID {
			continue
		}
		if match != 0 {
			return 0, fmt.Errorf("Message-ID is ambiguous in the selected mailbox")
		}
		match = uid
	}
	if match == 0 {
		if !strings.ContainsAny(messageID, "<>") {
			return 0, fmt.Errorf("%w; copy the listed Message-ID verbatim, including angle brackets", ErrEvidenceNotFound)
		}
		return 0, ErrEvidenceNotFound
	}
	return match, nil
}

func (c *Client) fetchEvidence(uid imap.UID, fullBody bool) (Evidence, error) {
	section := &imap.FetchItemBodySection{Peek: true}
	if !fullBody {
		section.Specifier = imap.PartSpecifierHeader
	}
	data, err := c.client.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{
		Flags: true, BodySection: []*imap.FetchItemBodySection{section},
	}).Collect()
	if err != nil {
		return Evidence{}, err
	}
	if len(data) != 1 || len(data[0].FindBodySection(section)) == 0 {
		return Evidence{}, fmt.Errorf("message disappeared during read")
	}
	raw := data[0].FindBodySection(section)
	headers, err := evidenceHeaders(raw)
	if err != nil {
		return Evidence{}, err
	}
	return Evidence{Headers: headers, HeadersAndBody: raw, Seen: slices.Contains(data[0].Flags, imap.FlagSeen)}, nil
}

func evidenceHeaders(raw []byte) (mail.Header, error) {
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode message headers: %w", err)
	}
	return message.Header, nil
}
