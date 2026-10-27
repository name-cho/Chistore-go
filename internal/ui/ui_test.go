package ui

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func newTestUI(input string, color, yes bool) (*UI, *bytes.Buffer) {
	out := &bytes.Buffer{}
	return New(strings.NewReader(input), out, out, color, yes), out
}

func TestConfirm(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"YES\n", true},
		{" yes \n", true},
		{"n\n", false},
		{"\n", false},
		{"", false},
	}
	for _, tc := range cases {
		u, _ := newTestUI(tc.input, false, false)
		got, err := u.Confirm("Proceed?")
		if err != nil {
			t.Fatalf("Confirm(%q) error = %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("Confirm(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestConfirmYesFlagSkipsInput(t *testing.T) {
	u, out := newTestUI("", false, true)
	got, err := u.Confirm("Proceed?")
	if err != nil || !got {
		t.Fatalf("Confirm() = %v, %v", got, err)
	}
	if strings.Contains(out.String(), "Proceed?") {
		t.Errorf("prompt printed despite --yes: %q", out.String())
	}
}

func TestAsk(t *testing.T) {
	u, out := newTestUI("  skip me  \n", false, false)
	got, err := u.Ask("Skip: ")
	if err != nil || got != "skip me" {
		t.Fatalf("Ask() = %q, %v", got, err)
	}
	if !strings.Contains(out.String(), "Skip: ") {
		t.Errorf("prompt not printed: %q", out.String())
	}
}

func TestMessages(t *testing.T) {
	u, out := newTestUI("", false, false)
	u.Okf("installed %s", "demo")
	u.Warnf("careful")
	u.Errorf("failed")
	u.Infof("note")

	got := out.String()
	for _, want := range []string{"[+] installed demo", "[!] careful", "[x] failed", "[i] note"} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q does not contain %q", got, want)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("color codes present with Color=false: %q", got)
	}
}

func TestMessagesWithColor(t *testing.T) {
	u, out := newTestUI("", true, false)
	u.Okf("installed")
	if !strings.Contains(out.String(), "\x1b[") {
		t.Errorf("no ANSI codes with Color=true: %q", out.String())
	}
}

func TestReadErrorIsPropagated(t *testing.T) {
	u := New(errorReader{}, io.Discard, io.Discard, false, false)
	if _, err := u.Ask("x: "); err == nil {
		t.Fatal("Ask() error = nil")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
