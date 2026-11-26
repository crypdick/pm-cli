package imap

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func appendEvidence(t *testing.T, raw *imapclient.Client, messageID string) imap.UID {
	t.Helper()
	message := fmt.Sprintf("From: sender@example.test\r\nMessage-ID: %s\r\nDate: Thu, 02 Jan 2020 23:59:00 +0000\r\n\r\nbody\r\n", messageID)
	appendCommand := raw.Append("INBOX", int64(len(message)), nil)
	if _, err := appendCommand.Write([]byte(message)); err != nil {
		t.Fatal(err)
	}
	if err := appendCommand.Close(); err != nil {
		t.Fatal(err)
	}
	item, err := appendCommand.Wait()
	if err != nil {
		t.Fatal(err)
	}
	return item.UID
}

func TestEvidenceUsesSentDateAndDoesNotMarkRead(t *testing.T) {
	fx, _ := newIMAPFixture(t)
	id := "<evidence@example.test>"
	appendEvidence(t, fx.raw, id)
	options := EvidenceOptions{
		Mailbox: "INBOX", Limit: 1, Unread: true, FullBody: true,
		SentSince:  time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC),
		SentBefore: time.Date(2020, 1, 3, 0, 0, 0, 0, time.UTC),
	}
	for range 2 {
		var page []Evidence
		err := fx.client.WalkEvidence(options, func(item Evidence) error {
			page = append(page, item)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(page) != 1 || page[0].Seen || page[0].Headers.Get("Message-ID") != id {
			t.Fatalf("wanted one unread message filtered by its sent date, got %v", page)
		}
		item, err := fx.client.ReadEvidence("INBOX", id)
		if err != nil || item.Seen || !strings.HasSuffix(string(item.HeadersAndBody), "body\r\n") {
			t.Fatalf("read-only evidence failed: %+v, %v", item, err)
		}
	}
	if _, err := fx.client.ReadEvidence("INBOX", "evidence@example"); !errors.Is(err, ErrEvidenceNotFound) {
		t.Fatalf("a substring must not select a message: %v", err)
	}
	appendEvidence(t, fx.raw, id)
	if _, err := fx.client.ReadEvidence("INBOX", id); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("duplicate Message-ID must fail closed: %v", err)
	}
}

func TestExpungeEvidenceLeavesOtherDeletedMessages(t *testing.T) {
	fx, _ := newIMAPFixtureWithCaps(t, imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}})
	target := "<target@example.test>"
	other := "<other@example.test>"
	appendEvidence(t, fx.raw, target)
	otherUID := appendEvidence(t, fx.raw, other)
	if _, err := fx.client.SelectMailbox("INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := fx.raw.Store(imap.UIDSetNum(otherUID), &imap.StoreFlags{
		Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted},
	}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	if err := fx.client.ExpungeEvidence("INBOX", target); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{target: false, other: true} {
		if exists, err := fx.client.EvidenceExists("INBOX", id); err != nil || exists != want {
			t.Fatalf("exists(%s) = %t, %v, want %t", id, exists, err, want)
		}
	}
}
