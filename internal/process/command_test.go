package process

import (
	"reflect"
	"testing"
)

func TestCommandArgs_QuotesAndExpandsInjectedEnvironment(t *testing.T) {
	got, err := commandArgs(
		`python3 -m http.server "$PORT" --directory "site files"`,
		map[string]string{"PORT": "28081"},
	)
	if err != nil {
		t.Fatalf("commandArgs: %v", err)
	}

	want := []string{
		"python3",
		"-m",
		"http.server",
		"28081",
		"--directory",
		"site files",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commandArgs = %#v, want %#v", got, want)
	}
}

func TestCommandArgs_EscapedEnvironmentReferenceStaysLiteral(t *testing.T) {
	got, err := commandArgs(`tool \$PORT`, map[string]string{"PORT": "28081"})
	if err != nil {
		t.Fatalf("commandArgs: %v", err)
	}

	want := []string{"tool", "$PORT"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commandArgs = %#v, want %#v", got, want)
	}
}

func TestCommandArgs_RejectsUnclosedQuote(t *testing.T) {
	if _, err := commandArgs(`tool "unfinished`, nil); err == nil {
		t.Fatal("commandArgs should reject an unclosed quote")
	}
}
