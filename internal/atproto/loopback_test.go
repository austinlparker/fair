package atproto_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"aparker.io/fair/internal/atproto"
)

func TestLoopback_RedirectURIPointsAtRandomLocalPort(t *testing.T) {
	srv, err := atproto.StartLoopback()
	if err != nil {
		t.Fatalf("StartLoopback: %v", err)
	}
	defer srv.Close()

	uri := srv.RedirectURI()
	if !strings.HasPrefix(uri, "http://127.0.0.1:") {
		t.Errorf("RedirectURI %q should start with http://127.0.0.1:", uri)
	}
	if !strings.HasSuffix(uri, "/callback") {
		t.Errorf("RedirectURI %q should end with /callback", uri)
	}
}

func TestLoopback_CallbackDeliversCodeStateIss(t *testing.T) {
	srv, err := atproto.StartLoopback()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	go func() {
		// give the server a moment to be in the Accept loop
		time.Sleep(20 * time.Millisecond)
		resp, err := http.Get(srv.RedirectURI() + "?code=THE_CODE&state=THE_STATE&iss=https%3A%2F%2Fbsky.social")
		if err != nil {
			t.Errorf("GET callback: %v", err)
			return
		}
		// Confirm browser sees a friendly success page
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !strings.Contains(strings.ToLower(string(body)), "you can close") {
			t.Errorf("expected close-this-tab page, got:\n%s", string(body))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := srv.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Code != "THE_CODE" {
		t.Errorf("Code: %q", result.Code)
	}
	if result.State != "THE_STATE" {
		t.Errorf("State: %q", result.State)
	}
	if result.Iss != "https://bsky.social" {
		t.Errorf("Iss: %q", result.Iss)
	}
	if result.Error != "" {
		t.Errorf("Error: %q (expected empty)", result.Error)
	}
}

func TestLoopback_CallbackSurfacesAuthError(t *testing.T) {
	srv, err := atproto.StartLoopback()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	go func() {
		time.Sleep(20 * time.Millisecond)
		resp, _ := http.Get(srv.RedirectURI() + "?error=access_denied&error_description=user+rejected&state=ST")
		if resp != nil {
			resp.Body.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := srv.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait should succeed even on auth error (caller decides): %v", err)
	}
	if result.Error != "access_denied" {
		t.Errorf("Error: %q", result.Error)
	}
	if result.State != "ST" {
		t.Errorf("State: %q", result.State)
	}
}

func TestLoopback_WaitRespectsContextCancel(t *testing.T) {
	srv, err := atproto.StartLoopback()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = srv.Wait(ctx)
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected context error, got %v", err)
	}
}

func TestLoopback_CloseUnblocksWait(t *testing.T) {
	srv, err := atproto.StartLoopback()
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, werr := srv.Wait(context.Background())
		done <- werr
	}()

	time.Sleep(20 * time.Millisecond)
	srv.Close()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected non-nil error after Close()")
		}
	case <-time.After(time.Second):
		t.Error("Wait did not return after Close()")
	}
}
