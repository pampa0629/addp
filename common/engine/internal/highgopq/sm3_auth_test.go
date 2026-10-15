package pq

import "testing"

func TestSM3KnownVector(t *testing.T) {
	if got := Sm3ToString("abc"); got != "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0" {
		t.Fatalf("SM3 known vector mismatch: %s", got)
	}
}

func TestHighGoPasswordResponseGolden(t *testing.T) {
	// Independently generated with OpenSSL SM3 (Python hashlib), not this driver.
	got := "sm3" + Sm3ToString(Sm3ToString("fixture-passworduser")+"\x01\x02\x03\x04")
	const want = "sm3b60121e46a4ec353a07a6f1cf0981a25ae532b6e7f8162d0cacab209424b113e"
	if got != want {
		t.Fatal("HighGo password response does not match independent vector")
	}
}
