package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/name-cho/chistore-go/internal/cli"
	"github.com/name-cho/chistore-go/internal/manager"
	"github.com/name-cho/chistore-go/internal/metadata"
	"github.com/name-cho/chistore-go/internal/repos"
	"github.com/name-cho/chistore-go/internal/state"
)

func (a *App) runManagerCommand(ctx context.Context) error {
	switch a.cmd.Name {
	case cli.NameSearch:
		return a.runSearch(ctx)
	case cli.NameInstall:
		return a.runInstall(ctx)
	case cli.NameUpgrade:
		return a.runUpgrade(ctx)
	case cli.NameRemove, cli.NamePurge:
		return a.runRemove(ctx)
	case cli.NameList:
		return a.runList()
	case cli.NameSearchInstalled:
		return a.runSearchInstalled()
	case cli.NameInfo:
		return a.runInfo(ctx)
	default:
		return fmt.Errorf("unsupported command %s", a.cmd.Name)
	}
}

func (a *App) runRepo(ctx context.Context) error {
	switch a.cmd.Name {
	case cli.NameRepoAdd:
		name, url := a.cmd.Args[0], a.cmd.Args[1]
		priority := a.cmd.Priority
		if priority == 0 {
			priority = repos.DefaultPriority
		}
		if err := a.repoConfig.Add(name, url, priority); err != nil {
			return err
		}
		if err := a.repoConfig.Save(a.paths.ReposFile()); err != nil {
			return err
		}
		a.ui.Okf("Repository %s added", name)
		return nil
	case cli.NameRepoRemove:
		name := a.cmd.Args[0]
		if err := a.repoConfig.Remove(name); err != nil {
			return err
		}
		if err := a.repoConfig.Save(a.paths.ReposFile()); err != nil {
			return err
		}
		a.ui.Okf("Repository %s removed", name)
		return nil
	case cli.NameRepoList:
		return a.listRepos()
	case cli.NameRepoRefresh:
		return a.refreshRepos(ctx)
	default:
		return fmt.Errorf("unsupported command %s", a.cmd.Name)
	}
}

func (a *App) listRepos() error {
	if a.repoOverride != "" {
		a.ui.Infof("Repository override is active: %s", a.repoOverride)
	}
	if len(a.repoConfig.Repos) == 0 {
		a.ui.Infof("No repositories configured.")
		return nil
	}
	a.ui.Printf("\n%s\n", a.ui.Bold("Repositories:"))
	for _, entry := range a.repoConfig.Repos {
		status := "enabled"
		if !entry.IsEnabled() {
			status = "disabled"
		}
		a.ui.Printf("  %-16s priority %-4d %-9s %s\n", entry.Name, entry.Priority, status, entry.URL)
	}
	a.ui.Printf("\n")
	return nil
}

func (a *App) refreshRepos(ctx context.Context) error {
	if a.repos == nil {
		return fmt.Errorf("repository access is not configured")
	}
	statuses := a.repos.Status(ctx)
	a.ui.Printf("\n%s\n", a.ui.Bold("Repositories:"))
	failures := 0
	for _, status := range statuses {
		if status.Err != nil {
			a.ui.Warnf("%s: %v", status.Repo.Name, status.Err)
			failures++
			continue
		}
		a.ui.Printf("  %-16s %d package(s)\n", status.Repo.Name, status.Packages)
	}
	a.ui.Printf("\n")
	if failures == len(statuses) {
		return fmt.Errorf("no repository could be read")
	}
	return nil
}

func (a *App) runSearch(ctx context.Context) error {
	query := strings.Join(a.cmd.Args, " ")
	if query == "" {
		a.ui.Infof("Packages in repositories:")
	} else {
		a.ui.Infof("Searching repositories: %s", a.ui.Bold(query))
	}
	packages, err := a.manager.Search(ctx, query)
	if err != nil {
		return err
	}
	if len(packages) == 0 {
		a.ui.Warnf("No packages found.")
		return nil
	}
	for _, pkg := range packages {
		a.printPackage(pkg)
	}
	return nil
}

func (a *App) runInstall(ctx context.Context) error {
	for _, id := range a.cmd.Args {
		pkg, err := a.manager.Info(ctx, id)
		if err != nil {
			return err
		}
		a.printPackage(pkg)

		confirmed, err := a.ui.Confirm(fmt.Sprintf("Install %s?", a.ui.Bold(pkg.ID)))
		if err != nil {
			return err
		}
		if !confirmed {
			a.ui.Infof("Cancelled")
			continue
		}
		if err := a.manager.Install(ctx, pkg); err != nil {
			if errors.Is(err, manager.ErrCancelled) {
				a.ui.Infof("Cancelled")
				continue
			}
			return err
		}
	}
	return nil
}

func (a *App) runUpgrade(ctx context.Context) error {
	upgrades, err := a.manager.Upgrades(ctx)
	if err != nil {
		return err
	}
	if len(upgrades) == 0 {
		a.ui.Infof("All packages are up to date.")
		return nil
	}

	a.ui.Printf("\n%s\n", a.ui.Bold("Available upgrades:"))
	for _, upgrade := range upgrades {
		a.ui.Printf("  %s %s %s %s @%s\n",
			a.ui.Green(upgrade.Installed.ID),
			upgrade.Installed.Version,
			a.ui.Yellow("->"),
			upgrade.Remote.Version,
			upgrade.Remote.Repo,
		)
	}

	skip, cancelled, err := a.upgradeSkips()
	if err != nil {
		return err
	}
	if cancelled {
		a.ui.Infof("Upgrade cancelled.")
		return nil
	}

	failures := 0
	for _, upgrade := range upgrades {
		if skip[upgrade.Installed.ID] {
			a.ui.Warnf("Skipping %s", upgrade.Installed.ID)
			continue
		}
		if err := a.manager.Upgrade(ctx, upgrade); err != nil {
			a.ui.Errorf("%v", err)
			failures++
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d package(s) failed to update", failures)
	}
	a.ui.Okf("System upgrade complete.")
	return nil
}

func (a *App) upgradeSkips() (map[string]bool, bool, error) {
	skip := map[string]bool{}
	if a.cmd.Yes {
		return skip, false, nil
	}
	input, err := a.ui.Ask("Skip packages (comma/space) or 'n' to cancel: ")
	if errors.Is(err, io.EOF) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if strings.EqualFold(input, "n") {
		return nil, true, nil
	}
	for _, name := range strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	}) {
		if name != "" {
			skip[name] = true
		}
	}
	return skip, false, nil
}

func (a *App) runRemove(ctx context.Context) error {
	purge := a.cmd.Name == cli.NamePurge
	for _, id := range a.cmd.Args {
		entry, ok := a.manager.Installed(id)
		if !ok {
			a.ui.Warnf("%s is not installed", id)
			continue
		}
		a.printInstalled(entry, purge)

		verb := "Remove"
		if purge {
			verb = "Fully remove"
		}
		confirmed, err := a.ui.Confirm(fmt.Sprintf("%s %s?", verb, a.ui.Bold(id)))
		if err != nil {
			return err
		}
		if !confirmed {
			a.ui.Infof("Cancelled")
			continue
		}
		if err := a.manager.Remove(ctx, id, purge); err != nil {
			if errors.Is(err, manager.ErrNotInstalled) {
				a.ui.Warnf("%s is not installed", id)
				continue
			}
			return err
		}
	}
	return nil
}

func (a *App) runList() error {
	packages := a.manager.List()
	if len(packages) == 0 {
		a.ui.Infof("No installed tools.")
		return nil
	}
	a.ui.Printf("%s\n", a.ui.Bold("Installed:"))
	for _, pkg := range packages {
		repo := ""
		if pkg.Repo != "" {
			repo = " @" + pkg.Repo
		}
		a.ui.Printf("  %s v%s%s | Installed: %s\n", a.ui.Green(pkg.ID), pkg.Version, repo, pkg.InstalledAt)
	}
	return nil
}

func (a *App) runSearchInstalled() error {
	query := strings.Join(a.cmd.Args, " ")
	packages := a.manager.SearchInstalled(query)
	if len(packages) == 0 {
		a.ui.Warnf("No installed packages match.")
		return nil
	}
	if query == "" {
		a.ui.Printf("%s\n", a.ui.Bold("Installed:"))
	} else {
		a.ui.Printf("%s\n", a.ui.Bold("Installed (match: "+query+"):"))
	}
	for _, pkg := range packages {
		a.ui.Printf("  %s v%s\n", a.ui.Green(pkg.ID), pkg.Version)
	}
	return nil
}

func (a *App) runInfo(ctx context.Context) error {
	pkg, err := a.manager.Info(ctx, a.cmd.Args[0])
	if err != nil {
		return err
	}
	a.printPackage(pkg)
	return nil
}

func (a *App) printPackage(pkg metadata.Package) {
	a.ui.Printf("\n%s %s | %s [%s]\n",
		a.ui.Bold(pkg.Name),
		a.ui.Cyan("v"+pkg.Version),
		pkg.Size,
		pkg.Kind,
	)
	a.ui.Printf("  %s\n", pkg.Description)
	if pkg.Author != "" && pkg.Author != "Unknown" {
		a.ui.Printf("  Author: %s\n", pkg.Author)
	}
	if pkg.Homepage != "" && pkg.Homepage != "none" {
		a.ui.Printf("  Homepage: %s\n", pkg.Homepage)
	}
	if pkg.Source != "" {
		a.ui.Printf("  Source: %s\n", pkg.Source)
	}
	if pkg.Repo != "" {
		a.ui.Printf("  Repository: %s\n", pkg.Repo)
	}
	a.ui.Printf("\n")
}

func (a *App) printInstalled(pkg state.Package, purge bool) {
	a.ui.Printf("\n%s %s\n", a.ui.Bold("Remove:"), a.ui.Bold(pkg.ID))
	a.ui.Printf("  Version: %s\n", pkg.Version)
	if pkg.Binary != "" {
		a.ui.Printf("  Binary: %s\n", pkg.Binary)
	}
	if purge {
		if pkg.ConfigDir != "" {
			a.ui.Printf("  Config: %s\n", pkg.ConfigDir)
		}
		if pkg.DataDir != "" {
			a.ui.Printf("  Data: %s\n", pkg.DataDir)
		}
	}
	a.ui.Printf("\n")
}

func (a *App) printHelp() {
	a.ui.Printf("%s", helpText)
}

const helpText = `chistore - user utility package manager

Usage:
  chistore -Ss [query]        Search repositories
  chistore -S <pkg...>        Install package(s)
  chistore -Syu               Upgrade installed packages
  chistore -Rn <pkg...>       Remove (keep configs)
  chistore -Rns <pkg...>      Remove completely (purge)
  chistore -Qet               List installed
  chistore -Qs [query]        Search installed
  chistore -Qi <pkg>          Show package info

Readable commands:
  chistore search [query]     Search repositories
  chistore install <pkg...>   Install package(s), pkg@repo picks a repository
  chistore upgrade            Upgrade installed packages
  chistore remove <pkg...>    Remove (keep configs)
  chistore purge <pkg...>     Remove completely
  chistore list               List installed
  chistore info <pkg>         Show package info
  chistore self-update        Update chistore itself

Repositories:
  chistore repo add <name> <url> [--priority N]
  chistore repo list
  chistore repo remove <name>
  chistore repo refresh

Package authoring (run in the package repository):
  chistore pkg init <id>      Create a package and rebuild the index
  chistore pkg index          Rebuild index.json from package directories
  chistore pkg validate       Validate the index, metadata and payloads

Options:
  -y, --yes                   Do not ask for confirmation
  --no-color                  Disable colors
  --repo URL                  Use a single repository, ignoring repos.json
  --priority N                Priority for repo add (lower wins, default 10)
  --no-self-update            Skip the daily self-update check
  --force                     Force self-update for a development build
`
