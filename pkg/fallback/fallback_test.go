package fallback

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

// quietLogger discards everything; tests don't assert on log output.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestWalk_FirstErrorsSecondSucceeds — first tier errors, second
// succeeds; the second's Output wins and the Source field is stamped.
func TestWalk_FirstErrorsSecondSucceeds(t *testing.T) {
	want := Output{EurPerM2Cents: 12345, LevelUsed: "city", SampleSize: 42}
	ladder := []Tier{
		{
			Name: "primary",
			Try: func(_ context.Context, _ Input) (Output, error) {
				return Output{}, errors.New("boom")
			},
		},
		{
			Name: "backup",
			Try: func(_ context.Context, _ Input) (Output, error) {
				return want, nil
			},
		},
	}
	got, err := Walk(context.Background(), quietLogger(), ladder, Input{})
	if err != nil {
		t.Fatalf("Walk: unexpected err %v", err)
	}
	if got.EurPerM2Cents != want.EurPerM2Cents || got.SampleSize != want.SampleSize || got.LevelUsed != want.LevelUsed {
		t.Errorf("Walk got %+v want %+v", got, want)
	}
	if got.Source != "backup" {
		t.Errorf("Source = %q want %q", got.Source, "backup")
	}
}

// TestWalk_AllSkipped — every tier returns success but each is filtered
// out by SkipOn; Walk returns ErrNoTierSucceeded with the skip reasons
// joined.
func TestWalk_AllSkipped(t *testing.T) {
	skipAll := func(Output) bool { return true }
	ladder := []Tier{
		{Name: "a", Try: func(_ context.Context, _ Input) (Output, error) { return Output{SampleSize: 0}, nil }, SkipOn: skipAll},
		{Name: "b", Try: func(_ context.Context, _ Input) (Output, error) { return Output{SampleSize: 0}, nil }, SkipOn: skipAll},
		{Name: "c", Try: func(_ context.Context, _ Input) (Output, error) { return Output{SampleSize: 0}, nil }, SkipOn: skipAll},
	}
	_, err := Walk(context.Background(), quietLogger(), ladder, Input{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNoTierSucceeded) {
		t.Errorf("error chain missing ErrNoTierSucceeded: %v", err)
	}
	// All three tier names should be present in the joined error
	// message so an operator can see which rungs ran.
	for _, name := range []string{`"a"`, `"b"`, `"c"`} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("err %q missing tier name %s", err.Error(), name)
		}
	}
}

// TestWalk_ZeroSampleTriggersSkip — first tier succeeds with sample==0
// and SkipOn fires; second tier wins.
func TestWalk_ZeroSampleTriggersSkip(t *testing.T) {
	skipIfEmpty := func(o Output) bool { return o.SampleSize == 0 }
	ladder := []Tier{
		{
			Name: "primary",
			Try: func(_ context.Context, _ Input) (Output, error) {
				return Output{LevelUsed: "address", SampleSize: 0}, nil
			},
			SkipOn: skipIfEmpty,
		},
		{
			Name: "city",
			Try: func(_ context.Context, _ Input) (Output, error) {
				return Output{EurPerM2Cents: 999, LevelUsed: "city", SampleSize: 5}, nil
			},
			SkipOn: skipIfEmpty,
		},
	}
	got, err := Walk(context.Background(), quietLogger(), ladder, Input{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if got.Source != "city" {
		t.Errorf("Source = %q want %q", got.Source, "city")
	}
	if got.LevelUsed != "city" || got.SampleSize != 5 {
		t.Errorf("got %+v, expected city/5", got)
	}
}

// TestWalk_EmptyLadder — no tiers means an immediate ErrNoTierSucceeded.
func TestWalk_EmptyLadder(t *testing.T) {
	_, err := Walk(context.Background(), quietLogger(), nil, Input{})
	if err == nil || !errors.Is(err, ErrNoTierSucceeded) {
		t.Fatalf("Walk: want ErrNoTierSucceeded, got %v", err)
	}
}

// TestWalk_ContextCancelledBetweenTiers — when ctx is cancelled before
// the next tier runs, Walk surfaces the ctx error verbatim.
func TestWalk_ContextCancelledBetweenTiers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ladder := []Tier{
		{
			Name: "primary",
			Try: func(_ context.Context, _ Input) (Output, error) {
				cancel()
				return Output{}, errors.New("boom")
			},
		},
		{
			Name: "secondary",
			Try: func(_ context.Context, _ Input) (Output, error) {
				t.Fatal("secondary should not run after ctx cancelled")
				return Output{}, nil
			},
		},
	}
	_, err := Walk(ctx, quietLogger(), ladder, Input{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestWalk_NilSkipOnAcceptsAnyOutput — a tier whose SkipOn is nil wins
// even with a zero-sample output.
func TestWalk_NilSkipOnAcceptsAnyOutput(t *testing.T) {
	ladder := []Tier{
		{
			Name: "fallback",
			Try: func(_ context.Context, _ Input) (Output, error) {
				return Output{LevelUsed: "department", SampleSize: 0}, nil
			},
			SkipOn: nil,
		},
	}
	got, err := Walk(context.Background(), quietLogger(), ladder, Input{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if got.Source != "fallback" {
		t.Errorf("Source = %q", got.Source)
	}
}
