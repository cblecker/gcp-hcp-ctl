package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMainExitsNonzeroOnCommandFailure(t *testing.T) {
	if os.Getenv("GCPHCPCTL_MAIN_TEST_CHILD") == "1" {
		os.Args = []string{"gcphcpctl", "--config", os.Getenv("GCPHCPCTL_MAIN_TEST_CONFIG"), "--api-endpoint", "http://example.invalid", "--project", "my-project", "cluster", "list"}
		main()
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMainExitsNonzeroOnCommandFailure$")
	tmp := t.TempDir()
	// Select the silent gcloud fallback without using the host's ADC or
	// invoking gcloud; endpoint validation fails before token acquisition.
	adcFile := filepath.Join(tmp, "adc.json")
	if err := os.WriteFile(adcFile, []byte(`{"type":"authorized_user"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(os.Environ(), "GCPHCPCTL_MAIN_TEST_CHILD=1", "GCPHCPCTL_MAIN_TEST_CONFIG="+filepath.Join(tmp, "missing-config.yaml"), "HOME="+tmp, "GOOGLE_APPLICATION_CREDENTIALS="+adcFile, "GCPHCPCTL_API_ENDPOINT=", "GCPHCPCTL_PROJECT=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("exit = %v, stdout = %q, stderr = %q", err, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	got := stderr.String()
	if strings.Count(got, "API endpoint must use HTTPS") != 1 || !strings.HasPrefix(got, "API endpoint must use HTTPS:") || strings.Contains(got, "No clusters found") {
		t.Fatalf("unexpected command diagnostic: %q", got)
	}
}
