package cli

import (
	"strings"
	"testing"
)

func TestParseLegacyFlags(t *testing.T) {
	cases := []struct {
		args []string
		name string
		rest string
	}{
		{[]string{"-h"}, NameHelp, ""},
		{[]string{"-Ss", "env"}, NameSearch, "env"},
		{[]string{"-S", "envsnap"}, NameInstall, "envsnap"},
		{[]string{"-Syu"}, NameUpgrade, ""},
		{[]string{"-Rn", "envsnap"}, NameRemove, "envsnap"},
		{[]string{"-Rns", "envsnap"}, NamePurge, "envsnap"},
		{[]string{"-Qet"}, NameList, ""},
		{[]string{"-Qs", "env"}, NameSearchInstalled, "env"},
		{[]string{"-Qi", "envsnap"}, NameInfo, "envsnap"},
	}
	for _, tc := range cases {
		command, err := Parse(tc.args)
		if err != nil {
			t.Fatalf("Parse(%v) error = %v", tc.args, err)
		}
		if command.Name != tc.name || strings.Join(command.Args, " ") != tc.rest {
			t.Errorf("Parse(%v) = %s %v", tc.args, command.Name, command.Args)
		}
	}
}

func TestParseSubcommands(t *testing.T) {
	cases := []struct {
		args []string
		name string
		rest string
	}{
		{[]string{"search", "env", "snap"}, NameSearch, "env snap"},
		{[]string{"install", "a", "b"}, NameInstall, "a b"},
		{[]string{"remove", "a"}, NameRemove, "a"},
		{[]string{"purge", "a"}, NamePurge, "a"},
		{[]string{"list"}, NameList, ""},
		{[]string{"upgrade"}, NameUpgrade, ""},
		{[]string{"info", "a"}, NameInfo, "a"},
		{[]string{"self-update"}, NameSelfUpdate, ""},
		{[]string{"pkg", "init", "demo"}, NamePkgInit, "demo"},
		{[]string{"pkg", "index"}, NamePkgIndex, ""},
		{[]string{"pkg", "validate"}, NamePkgValidate, ""},
		{[]string{"repo", "add", "mirror", "https://example.com"}, NameRepoAdd, "mirror https://example.com"},
		{[]string{"repo", "list"}, NameRepoList, ""},
		{[]string{"repo", "remove", "mirror"}, NameRepoRemove, "mirror"},
		{[]string{"repo", "refresh"}, NameRepoRefresh, ""},
		{[]string{"version"}, NameVersion, ""},
	}
	for _, tc := range cases {
		command, err := Parse(tc.args)
		if err != nil {
			t.Fatalf("Parse(%v) error = %v", tc.args, err)
		}
		if command.Name != tc.name || strings.Join(command.Args, " ") != tc.rest {
			t.Errorf("Parse(%v) = %s %v", tc.args, command.Name, command.Args)
		}
	}
}

func TestParseGlobalFlags(t *testing.T) {
	command, err := Parse([]string{"--yes", "install", "--no-color", "demo", "--repo", "https://example.com/repo", "--no-self-update", "--force"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !command.Yes || !command.NoColor || !command.NoSelfUpdate || !command.Force {
		t.Errorf("flags not applied: %+v", command)
	}
	if command.RepoURL != "https://example.com/repo" {
		t.Errorf("RepoURL = %q", command.RepoURL)
	}
	if command.Name != NameInstall || strings.Join(command.Args, " ") != "demo" {
		t.Errorf("command = %s %v", command.Name, command.Args)
	}
}

func TestParseRepoEquals(t *testing.T) {
	command, err := Parse([]string{"--repo=https://example.com", "list"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if command.RepoURL != "https://example.com" || command.Name != NameList {
		t.Errorf("command = %+v", command)
	}
}

func TestParseRepoPriority(t *testing.T) {
	command, err := Parse([]string{"repo", "add", "mirror", "https://example.com", "--priority", "5"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if command.Name != NameRepoAdd || command.Priority != 5 || strings.Join(command.Args, " ") != "mirror https://example.com" {
		t.Errorf("command = %+v", command)
	}

	command, err = Parse([]string{"--priority=7", "repo", "add", "mirror", "https://example.com"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if command.Priority != 7 {
		t.Errorf("priority = %d", command.Priority)
	}
}

func TestParseErrors(t *testing.T) {
	cases := [][]string{
		{},
		{"nope"},
		{"install"},
		{"remove"},
		{"purge"},
		{"info"},
		{"info", "a", "b"},
		{"pkg"},
		{"pkg", "nope"},
		{"pkg", "init"},
		{"pkg", "index", "extra"},
		{"repo"},
		{"repo", "add", "mirror"},
		{"repo", "add", "mirror", "https://example.com", "extra"},
		{"repo", "remove"},
		{"repo", "remove", "a", "b"},
		{"repo", "refresh", "extra"},
		{"repo", "nope"},
		{"--repo"},
		{"--priority"},
		{"--priority", "0", "repo", "list"},
		{"--unknown"},
	}
	for _, args := range cases {
		if _, err := Parse(args); err == nil {
			t.Errorf("Parse(%v) error = nil", args)
		}
	}
}

func TestParseDoubleDash(t *testing.T) {
	command, err := Parse([]string{"search", "--", "--yes"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if command.Yes || command.Name != NameSearch || strings.Join(command.Args, " ") != "--yes" {
		t.Errorf("command = %+v", command)
	}
}
