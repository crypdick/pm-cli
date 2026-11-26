package cli

import (
	"strings"
	"testing"
)

func TestEvidenceBodyExcludesTextAttachments(t *testing.T) {
	message := strings.Join([]string{
		"Content-Type: multipart/mixed; boundary=mail",
		"", "--mail",
		"Content-Type: text/html; charset=utf-8",
		"", "<p>Actual email</p>", "--mail",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Disposition: attachment; filename=attachment.txt",
		"", "Attachment, not the message body.", "--mail--", "",
	}, "\r\n")
	text, html := parseMessageBody([]byte(message))
	if text != "" || html != "<p>Actual email</p>" {
		t.Fatalf("expected only inline HTML, got text=%q html=%q", text, html)
	}
}
