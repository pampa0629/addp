package main

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestMatchTOTPAcceptsGeneratedCodeAndRejectsSecret(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_800_000_000, 0).UTC()
	code, err := totp.GenerateCode(secret, now)
	if err != nil {
		t.Fatalf("generate TOTP code: %v", err)
	}

	if _, valid := matchTOTP(secret, code, now); !valid {
		t.Fatal("generated 6-digit TOTP code should be valid")
	}
	if _, valid := matchTOTP(secret, secret, now); valid {
		t.Fatal("TOTP secret must not be accepted as a verification code")
	}
}

func TestIsTOTPCodeFormat(t *testing.T) {
	tests := []struct {
		name string
		code string
		want bool
	}{
		{name: "six digits", code: "012345", want: true},
		{name: "too short", code: "12345", want: false},
		{name: "too long", code: "1234567", want: false},
		{name: "letters", code: "12A456", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isTOTPCodeFormat(test.code); got != test.want {
				t.Fatalf("isTOTPCodeFormat(%q) = %t, want %t", test.code, got, test.want)
			}
		})
	}
}
