package cli

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	NameHelp            = "help"
	NameVersion         = "version"
	NameSearch          = "search"
	NameSearchInstalled = "search-installed"
	NameInstall         = "install"
	NameUpgrade         = "upgrade"
	NameRemove          = "remove"
	NamePurge           = "purge"
	NameList            = "list"
	NameInfo            = "info"
	NameSelfUpdate      = "self-update"
	NamePkgInit         = "pkg-init"
	NamePkgIndex        = "pkg-index"
	NamePkgValidate     = "pkg-validate"
	NameRepoAdd         = "repo-add"
	NameRepoList        = "repo-list"
	NameRepoRemove      = "repo-remove"
	NameRepoRefresh     = "repo-refresh"
)

type Command struct {
	Name         string
	Args         []string
	Yes          bool
	NoColor      bool
	NoSelfUpdate bool
	Force        bool
	RepoURL      string
	Priority     int
}

func Parse(args []string) (Command, error) {
	command, rest, err := extractFlags(args)
	if err != nil {
		return Command{}, err
	}
	if len(rest) == 0 {
		return Command{}, fmt.Errorf("no command given; run chistore -h")
	}

	name := rest[0]
	values := rest[1:]
	command.Args = values

	switch name {
	case "-h", "--help", "help":
		return command.only(NameHelp, values, 0)
	case "-V", "--version", "version":
		return command.only(NameVersion, values, 0)
	case "-Ss", "search":
		command.Name = NameSearch
		return command, nil
	case "-S", "install":
		return command.require(NameInstall, values, 1)
	case "-Syu", "upgrade":
		return command.only(NameUpgrade, values, 0)
	case "-Rn", "remove":
		return command.require(NameRemove, values, 1)
	case "-Rns", "purge":
		return command.require(NamePurge, values, 1)
	case "-Qet", "list":
		return command.only(NameList, values, 0)
	case "-Qs":
		command.Name = NameSearchInstalled
		return command, nil
	case "search-installed":
		command.Name = NameSearchInstalled
		return command, nil
	case "-Qi", "info":
		return command.exact(NameInfo, values, 1)
	case "self-update":
		return command.only(NameSelfUpdate, values, 0)
	case "pkg":
		return parsePkg(command, values)
	case "repo":
		return parseRepo(command, values)
	default:
		return Command{}, fmt.Errorf("unknown command %q; run chistore -h", name)
	}
}

func parsePkg(command Command, values []string) (Command, error) {
	if len(values) == 0 {
		return Command{}, fmt.Errorf("pkg needs a subcommand: init, index or validate")
	}
	sub := values[0]
	args := values[1:]
	command.Args = args
	switch sub {
	case "init", "new":
		return command.exact(NamePkgInit, args, 1)
	case "index":
		return command.only(NamePkgIndex, args, 0)
	case "validate", "check":
		return command.only(NamePkgValidate, args, 0)
	default:
		return Command{}, fmt.Errorf("unknown pkg subcommand %q", sub)
	}
}

func parseRepo(command Command, values []string) (Command, error) {
	if len(values) == 0 {
		return Command{}, fmt.Errorf("repo needs a subcommand: add, list, remove or refresh")
	}
	sub := values[0]
	args := values[1:]
	command.Args = args
	switch sub {
	case "add":
		if len(args) != 2 {
			return Command{}, fmt.Errorf("repo add needs a name and a URL")
		}
		command.Name = NameRepoAdd
		return command, nil
	case "list", "ls":
		return command.only(NameRepoList, args, 0)
	case "remove", "rm":
		return command.exact(NameRepoRemove, args, 1)
	case "refresh", "update", "sync":
		return command.only(NameRepoRefresh, args, 0)
	default:
		return Command{}, fmt.Errorf("unknown repo subcommand %q", sub)
	}
}

func (c Command) require(name string, args []string, minimum int) (Command, error) {
	if len(args) < minimum {
		return Command{}, fmt.Errorf("%s needs a package name", name)
	}
	c.Name = name
	return c, nil
}

func (c Command) exact(name string, args []string, count int) (Command, error) {
	if len(args) != count {
		return Command{}, fmt.Errorf("%s needs exactly %d argument(s)", name, count)
	}
	c.Name = name
	return c, nil
}

func (c Command) only(name string, args []string, count int) (Command, error) {
	if len(args) != count {
		return Command{}, fmt.Errorf("%s takes no arguments", name)
	}
	c.Name = name
	return c, nil
}

func extractFlags(args []string) (Command, []string, error) {
	var command Command
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		switch {
		case arg == "-y" || arg == "--yes":
			command.Yes = true
		case arg == "--no-color":
			command.NoColor = true
		case arg == "--no-self-update":
			command.NoSelfUpdate = true
		case arg == "--force":
			command.Force = true
		case arg == "--repo":
			i++
			if i >= len(args) {
				return Command{}, nil, fmt.Errorf("--repo needs a URL")
			}
			command.RepoURL = args[i]
		case strings.HasPrefix(arg, "--repo="):
			command.RepoURL = strings.TrimPrefix(arg, "--repo=")
			if command.RepoURL == "" {
				return Command{}, nil, fmt.Errorf("--repo needs a URL")
			}
		case arg == "--priority":
			i++
			if i >= len(args) {
				return Command{}, nil, fmt.Errorf("--priority needs a number")
			}
			priority, err := strconv.Atoi(args[i])
			if err != nil || priority <= 0 {
				return Command{}, nil, fmt.Errorf("--priority must be a positive number")
			}
			command.Priority = priority
		case strings.HasPrefix(arg, "--priority="):
			priority, err := strconv.Atoi(strings.TrimPrefix(arg, "--priority="))
			if err != nil || priority <= 0 {
				return Command{}, nil, fmt.Errorf("--priority must be a positive number")
			}
			command.Priority = priority
		default:
			rest = append(rest, arg)
		}
	}
	return command, rest, nil
}
