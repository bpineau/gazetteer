package gazetteer

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
)

func TestHTTPClientFrom_Default(t *testing.T) {
	c := HTTPClientFrom(context.Background())
	if c == nil {
		t.Fatal("HTTPClientFrom returned nil for empty ctx")
	}
}

func TestHTTPClientFrom_Set(t *testing.T) {
	want := &http.Client{}
	ctx := WithHTTPClient(context.Background(), want)
	got := HTTPClientFrom(ctx)
	if got != want {
		t.Errorf("HTTPClientFrom = %p, want %p", got, want)
	}
}

func TestLoggerFrom_Default(t *testing.T) {
	l := LoggerFrom(context.Background())
	if l == nil {
		t.Fatal("LoggerFrom returned nil for empty ctx")
	}
}

func TestLoggerFrom_Set(t *testing.T) {
	want := slog.Default().With("scope", "test")
	ctx := WithLogger(context.Background(), want)
	got := LoggerFrom(ctx)
	if got != want {
		t.Errorf("LoggerFrom returned a different logger")
	}
}

func TestDebugDumpFrom_DefaultsFalse(t *testing.T) {
	if DebugDumpFrom(context.Background()) {
		t.Errorf("DebugDumpFrom default = true, want false")
	}
}

func TestDebugDumpFrom_Set(t *testing.T) {
	ctx := WithDebugDump(context.Background(), true)
	if !DebugDumpFrom(ctx) {
		t.Errorf("DebugDumpFrom after WithDebugDump(true) = false")
	}
}

func TestCacheFrom_Default(t *testing.T) {
	c := CacheFrom(context.Background())
	if c == nil {
		t.Fatal("CacheFrom returned nil for empty ctx")
	}
}

func TestCacheFrom_Set(t *testing.T) {
	want := NewMemCache(5)
	ctx := WithCache(context.Background(), want)
	got := CacheFrom(ctx)
	if got != want {
		t.Errorf("CacheFrom returned wrong cache")
	}
}
