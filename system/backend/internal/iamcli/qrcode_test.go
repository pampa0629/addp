package iamcli

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintQRCodeRendersWithoutEchoingEnrollmentURI(t *testing.T) {
	const enrollmentURI = "otpauth://totp/ADDP:test-user?issuer=ADDP&secret=JBSWY3DPEHPK3PXP"
	var output bytes.Buffer
	if err := PrintQRCode(&output, enrollmentURI); err != nil {
		t.Fatalf("PrintQRCode() error = %v", err)
	}
	rendered := output.String()
	if strings.Contains(rendered, enrollmentURI) {
		t.Fatal("terminal QR output echoed the enrollment URI")
	}
	if !strings.Contains(rendered, "\x1b[40m") || !strings.Contains(rendered, "\x1b[47m") {
		t.Fatal("terminal QR output is missing black or white modules")
	}
	if strings.Count(rendered, "\n") < 20 {
		t.Fatal("terminal QR output is unexpectedly small")
	}
}
