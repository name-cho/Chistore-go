package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/name-cho/chistore-go/internal/cli"
	"github.com/name-cho/chistore-go/internal/config"
	"github.com/name-cho/chistore-go/internal/deps"
	"github.com/name-cho/chistore-go/internal/manager"
	"github.com/name-cho/chistore-go/internal/pkgtool"
	"github.com/name-cho/chistore-go/internal/repos"
	"github.com/name-cho/chistore-go/internal/selfupdate"
	"github.com/name-cho/chistore-go/internal/state"
	"github.com/name-cho/chistore-go/internal/ui"
)

const DefaultRepoURL = repos.DefaultURL

type Options struct {
	Version           string
	StdIn             io.Reader
	StdOut            io.Writer
	StdErr            io.Writer
	Getenv            func(string) string
	Home              string
	WorkDir           string
	GOOS              string
	HTTP              *http.Client
	Now               func() time.Time
	Executable        string
	UpdateURL         string
	DisableAutoUpdate bool
	DepsRunner        deps.Runner
}

type App struct {
	opts         Options
	cmd          cli.Command
	ui           *ui.UI
	paths        config.Paths
	repos        *repos.Set
	repoConfig   repos.Config
	repoOverride string
	manager      *manager.Manager
	update       *selfupdate.Service
}

func Run(ctx context.Context, args []string, opts Options) int {
	opts.setDefaults()
	command, err := cli.Parse(args)
	if err != nil {
		fmt.Fprintf(opts.StdErr, "[x] %v\n", err)
		return 1
	}

	app := &App{opts: opts, cmd: command}
	app.ui = ui.New(opts.StdIn, opts.StdOut, opts.StdErr, ui.SupportsColor(opts.StdOut, command.NoColor), command.Yes)
	if err := app.setup(); err != nil {
		app.ui.Errorf("%v", err)
		return 1
	}
	if err := app.execute(ctx); err != nil {
		app.ui.Errorf("%v", err)
		return 1
	}
	return 0
}

func (o *Options) setDefaults() {
	if o.Version == "" {
		o.Version = "dev"
	}
	if o.StdIn == nil {
		o.StdIn = os.Stdin
	}
	if o.StdOut == nil {
		o.StdOut = os.Stdout
	}
	if o.StdErr == nil {
		o.StdErr = os.Stderr
	}
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.WorkDir == "" {
		o.WorkDir = "."
	}
}

func (a *App) setup() error {
	switch a.cmd.Name {
	case cli.NameHelp, cli.NameVersion, cli.NamePkgInit, cli.NamePkgIndex, cli.NamePkgValidate:
		return nil
	}

	home := a.opts.Home
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("find home directory: %w", err)
		}
	}
	paths, err := config.Resolve(home, a.opts.Getenv, a.opts.GOOS)
	if err != nil {
		return err
	}

	needsRepo := commandNeedsRepo(a.cmd.Name)
	needsState := commandNeedsState(a.cmd.Name)
	if needsState {
		if err := paths.EnsureDirs(); err != nil {
			return err
		}
	}

	var store *state.Store
	if needsState {
		store, err = state.Open(paths.StateDir)
		if err != nil {
			return err
		}
	}

	override := a.cmd.RepoURL
	if override == "" {
		override = a.opts.Getenv("CHISTORE_REPO")
	}

	repoConfig := repos.DefaultConfig()
	if commandNeedsConfig(a.cmd.Name) || (needsRepo && override == "") {
		repoConfig, err = repos.Load(paths.ReposFile())
		if err != nil {
			return err
		}
	}

	var set *repos.Set
	if needsRepo {
		if override != "" {
			set, err = repos.NewSingle(repos.OverrideName, override, a.opts.HTTP)
		} else {
			set, err = repos.New(repoConfig, a.opts.HTTP)
		}
		if err != nil {
			return err
		}
	}

	runner := a.opts.DepsRunner
	if runner == nil {
		runner = deps.ExecRunner{Stdin: a.opts.StdIn, Stdout: a.opts.StdOut, Stderr: a.opts.StdErr}
	}
	dependencyInstaller := &deps.Installer{
		Runner:  runner,
		UI:      a.ui,
		VenvDir: paths.VenvDir,
		Python:  findPython(runner, a.opts.GOOS),
		Root:    os.Geteuid() == 0,
	}

	a.paths = paths
	a.repos = set
	a.repoConfig = repoConfig
	a.repoOverride = override
	a.manager = &manager.Manager{
		Repos:      set,
		Paths:      paths,
		State:      store,
		UI:         a.ui,
		Deps:       dependencyInstaller,
		DepsRunner: runner,
		Root:       os.Geteuid() == 0,
		Now:        a.opts.Now,
	}

	update, updateErr := a.newUpdateService()
	if updateErr != nil && a.cmd.Name == cli.NameSelfUpdate {
		return updateErr
	}
	a.update = update
	return nil
}

func commandNeedsRepo(name string) bool {
	switch name {
	case cli.NameSearch, cli.NameInstall, cli.NameUpgrade, cli.NameInfo, cli.NameRepoRefresh:
		return true
	default:
		return false
	}
}

func commandNeedsConfig(name string) bool {
	switch name {
	case cli.NameRepoAdd, cli.NameRepoList, cli.NameRepoRemove:
		return true
	default:
		return false
	}
}

func commandNeedsState(name string) bool {
	switch name {
	case cli.NameInstall, cli.NameUpgrade, cli.NameRemove, cli.NamePurge, cli.NameList, cli.NameSearchInstalled:
		return true
	default:
		return false
	}
}

func (a *App) newUpdateService() (*selfupdate.Service, error) {
	target := a.opts.Executable
	if target == "" {
		var err error
		target, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("find executable: %w", err)
		}
	}
	apiURL := a.opts.UpdateURL
	if apiURL == "" {
		apiURL = a.opts.Getenv("CHISTORE_UPDATE_URL")
	}
	if apiURL == "" {
		apiURL = selfupdate.DefaultAPIURL
	}
	return &selfupdate.Service{
		APIURL:         apiURL,
		HTTP:           a.opts.HTTP,
		TargetPath:     target,
		GOOS:           a.opts.GOOS,
		GOARCH:         runtime.GOARCH,
		CurrentVersion: a.opts.Version,
		StateDir:       a.paths.StateDir,
		Now:            a.opts.Now,
	}, nil
}

func (a *App) execute(ctx context.Context) error {
	switch a.cmd.Name {
	case cli.NameHelp:
		a.printHelp()
		return nil
	case cli.NameVersion:
		a.ui.Printf("chistore %s\n", a.opts.Version)
		return nil
	case cli.NamePkgInit, cli.NamePkgIndex, cli.NamePkgValidate:
		return a.runPkg()
	case cli.NameRepoAdd, cli.NameRepoList, cli.NameRepoRemove, cli.NameRepoRefresh:
		return a.runRepo(ctx)
	case cli.NameSelfUpdate:
		return a.runSelfUpdate(ctx)
	}

	a.maybeAutoUpdate(ctx)
	return a.runManagerCommand(ctx)
}

func (a *App) maybeAutoUpdate(ctx context.Context) {

	if !commandNeedsState(a.cmd.Name) {
		return
	}
	if a.update == nil || a.opts.DisableAutoUpdate || a.cmd.NoSelfUpdate || a.opts.Getenv("CHISTORE_NO_AUTO_UPDATE") == "1" {
		return
	}
	result, err := a.update.MaybeUpdate(ctx)
	if err != nil {
		return
	}
	if result.Updated {
		a.ui.Okf("chistore updated %s -> %s; restart to use the new version", result.From, result.To)
	}
}

func (a *App) runSelfUpdate(ctx context.Context) error {
	if a.update == nil {
		return fmt.Errorf("self-update is unavailable")
	}
	a.update.Force = a.cmd.Force
	result, err := a.update.Update(ctx)
	if err != nil {
		return err
	}
	if !result.Updated {
		a.ui.Infof("chistore %s is up to date", result.From)
		return nil
	}
	a.ui.Okf("chistore updated %s -> %s; restart to use the new version", result.From, result.To)
	return nil
}

func (a *App) runPkg() error {
	tool := pkgtool.New(a.opts.WorkDir)
	switch a.cmd.Name {
	case cli.NamePkgInit:
		id := a.cmd.Args[0]
		if err := tool.Init(id); err != nil {
			return err
		}
		a.ui.Okf("Created %s; edit %s/info.md, then run chistore pkg validate", id, id)
	case cli.NamePkgIndex:
		ids, err := tool.Index()
		if err != nil {
			return err
		}
		a.ui.Okf("INDEX rebuilt with %d package(s)", len(ids))
	case cli.NamePkgValidate:
		report, err := tool.Validate()
		if err != nil {
			return err
		}
		for _, warning := range report.Warnings {
			a.ui.Warnf("%s", warning)
		}
		for _, problem := range report.Errors {
			a.ui.Errorf("%s", problem)
		}
		if !report.OK() {
			return fmt.Errorf("validation failed with %d error(s)", len(report.Errors))
		}
		a.ui.Okf("Repository is valid")
	}
	return nil
}

func findPython(runner deps.Runner, goos string) string {
	candidates := []string{"python3", "python"}
	if goos == "windows" {
		candidates = []string{"python", "python3"}
	}
	for _, candidate := range candidates {
		if _, err := runner.LookPath(candidate); err == nil {
			return candidate
		}
	}
	return candidates[0]
}
