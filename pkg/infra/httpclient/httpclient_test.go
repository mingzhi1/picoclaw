package httpclient

import (
	"testing"
	"time"
)

func TestDefault_ReturnsNonNil(t *testing.T) {
	client := Default()
	if client == nil {
		t.Fatal("Default() returned nil")
	}
}

func TestNew_CustomTimeout(t *testing.T) {
	client := New(5 * time.Second)
	if client == nil {
		t.Fatal("New() returned nil")
	}
	if client.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", client.Timeout)
	}
}

func TestNewWithProxy_Empty_FallsBackToGlobal(t *testing.T) {
	client, err := NewWithProxy(10*time.Second, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("NewWithProxy returned nil")
	}
}

func TestNewWithProxy_ValidProxy(t *testing.T) {
	client, err := NewWithProxy(10*time.Second, "http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("NewWithProxy returned nil")
	}
}

func TestNewWithProxy_InvalidScheme(t *testing.T) {
	_, err := NewWithProxy(10*time.Second, "ftp://127.0.0.1:7890")
	if err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}

func TestNewWithProxy_InvalidURL(t *testing.T) {
	_, err := NewWithProxy(10*time.Second, "://bad-url")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestInit_SetsProxy(t *testing.T) {
	Init("http://test-proxy:1234")
	defer Init("") // reset

	if p := Proxy(); p != "http://test-proxy:1234" {
		t.Errorf("Proxy() = %q, want %q", p, "http://test-proxy:1234")
	}
}
