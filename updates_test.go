//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestManagerPackageChecksumsRequireManagerAndRuntime(t *testing.T) {
	digest := "29b477c57793d3f464988edbc8a0f27f9b5d77acb7c9f97c0d2605f8ca7ebf62"
	lines := []string{digest + "  KeeperLoader-Manager.exe"}
	for _, name := range requiredRuntimePayloadFiles {
		lines = append(lines, digest+"  runtime/"+name)
	}
	value, err := managerPackageChecksums(strings.Join(lines, "\r\n") + "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if value[strings.ToLower(managerExecutableName)] != digest {
		t.Fatalf("expected manager digest %s", digest)
	}
	if _, err = managerPackageChecksums("not-a-checksum  KeeperLoader-Manager.exe\n"); err == nil {
		t.Fatal("invalid checksum was accepted")
	}
}
