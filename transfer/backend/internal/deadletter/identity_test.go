package deadletter

import "testing"

func TestIdentityForSourceRecordIsStableAndPartitioned(t *testing.T) {
	const applyIdentity = "8aa1d865-8d56-4ac3-b9aa-59f50e575c37"
	first, err := IdentityForSourceRecord(applyIdentity, "addp://engine/9/path/orders?type=topic", "2", 41)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := IdentityForSourceRecord(applyIdentity, "addp://engine/9/path/orders?type=topic", "2", 41)
	if err != nil {
		t.Fatal(err)
	}
	if first != repeated {
		t.Fatalf("identity changed across retry: %s != %s", first, repeated)
	}
	otherPartition, _ := IdentityForSourceRecord(applyIdentity, "addp://engine/9/path/orders?type=topic", "3", 41)
	otherOffset, _ := IdentityForSourceRecord(applyIdentity, "addp://engine/9/path/orders?type=topic", "2", 42)
	if first == otherPartition || first == otherOffset || otherPartition == otherOffset {
		t.Fatalf("partition/offset identities collided: %s %s %s", first, otherPartition, otherOffset)
	}
	if first.Version() != 5 {
		t.Fatalf("identity version = %d, want UUID v5", first.Version())
	}
}

func TestIdentityForSourceRecordRejectsInvalidIdentity(t *testing.T) {
	if _, err := IdentityForSourceRecord("not-a-uuid", "source", "0", 0); err == nil {
		t.Fatal("invalid apply identity was accepted")
	}
	if _, err := IdentityForSourceRecord("8aa1d865-8d56-4ac3-b9aa-59f50e575c37", "source", "0", -1); err == nil {
		t.Fatal("negative offset was accepted")
	}
}
