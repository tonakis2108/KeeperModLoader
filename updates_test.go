//go:build windows

package main

import "testing"

func TestChecksumForManager(t *testing.T) {
	digest := "29b477c57793d3f464988edbc8a0f27f9b5d77acb7c9f97c0d2605f8ca7ebf62"
	value, err := checksumForManager(digest + "  KeeperLoader-Manager.exe\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if value != digest {
		t.Fatalf("expected %s, got %s", digest, value)
	}
	if _, err = checksumForManager("not-a-checksum  KeeperLoader-Manager.exe\n"); err == nil {
		t.Fatal("invalid checksum was accepted")
	}
}
