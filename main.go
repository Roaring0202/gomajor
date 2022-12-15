package main

import (
	"flag"
	"fmt"
	"go/token"
	"log"
	"os"
	"os/exec"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/sync/errgroup"

	"github.com/icholy/gomajor/internal/importpaths"
	"github.com/icholy/gomajor/internal/modproxy"
	"github.com/icholy/gomajor/internal/packages"
)

var help = `
GoMajor is a tool for major version upgrades

Usage:

    gomajor <command> [arguments]

The commands are:

    get     upgrade to a major version
    list    list available updates
    path    modify the module path
    help    show this help text
`

func main() {
	flag.Usage = func() {
		fmt.Println(help)
	}
	flag.Parse()
	switch flag.Arg(0) {
	case "get":
		if err := getcmd(flag.Args()[1:]); err != nil {
			log.Fatal(err)
		}
	case "list":
		if err := listcmd(flag.Args()[1:]); err != nil {
			log.Fatal(err)
		}
	case "path":
		if err := pathcmd(flag.Args()[1:]); err != nil {
			log.Fatal(err)
		}
	case "help", "":
		flag.Usage()
	default:
		fmt.Fprintf(os.Stderr, "unrecognized subcommand: %s\n", flag.Arg(0))
		flag.Usage()
	}
}

func listcmd(args []string) error {
	var dir string
	var pre, cached, major bool
	fset := flag.NewFlagSet("list", flag.ExitOnError)
	fset.BoolVar(&pre, "pre", false, "allow non-v0 prerelease versions")
	fset.StringVar(&dir, "dir", ".", "working directory")
	fset.BoolVar(&cached, "cached", true, "only fetch cached content from the module proxy")
	fset.BoolVar(&major, "major", false, "only show newer major versions")
	fset.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: gomajor list")
		fset.PrintDefaults()
	}
	fset.Parse(args)
	dependencies, err := packages.Direct(dir)
	if err != nil {
		return err
	}
	private := os.Getenv("GOPRIVATE")
	var group errgroup.Group
	if cached {
		group.SetLimit(3)
	} else {
		group.SetLimit(1)
	}
	for _, dep := range dependencies {
		dep := dep
		if module.MatchPrefixPatterns(private, dep.Path) {
			continue
		}
		group.Go(func() error {
			mod, err := modproxy.Latest(dep.Path, cached)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: failed: %v\n", dep.Path, err)
				return nil
			}
			v := mod.MaxVersion("", pre)
			if major && semver.Compare(semver.Major(v), semver.Major(dep.Version)) <= 0 {
				return nil
			}
			if semver.Compare(v, dep.Version) <= 0 {
				return nil
			}
			fmt.Printf("%s: %s [latest %v]\n", dep.Path, dep.Version, v)
			return nil
		})
	}
	return group.Wait()
}

func getcmd(args []string) error {
	var dir string
	var rewrite, goget, pre, cached bool
	fset := flag.NewFlagSet("get", flag.ExitOnError)
	fset.BoolVar(&pre, "pre", false, "allow non-v0 prerelease versions")
	fset.BoolVar(&rewrite, "rewrite", true, "rewrite import paths")
	fset.BoolVar(&goget, "get", true, "run go get")
	fset.StringVar(&dir, "dir", ".", "working directory")
	fset.BoolVar(&cached, "cached", true, "only fetch cached content from the module proxy")
	fset.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: gomajor get <pathspec>")
		fset.PrintDefaults()
	}
	fset.Parse(args)
	if fset.NArg() != 1 {
		return fmt.Errorf("missing package spec")
	}
	// split the package spec into its components
	pkgpath, query := packages.SplitSpec(fset.Arg(0))
	mod, err := modproxy.QueryPackage(pkgpath, cached)
	if err != nil {
		return err
	}
	modprefix := packages.ModPrefix(mod.Path)
	_, pkgdir, _ := packages.SplitPath(modprefix, pkgpath)
	// figure out what version to get
	var version string
	switch query {
	case "":
		version = mod.MaxVersion("", pre)
	case "latest":
		latest, err := modproxy.Latest(mod.Path, cached)
		if err != nil {
			return err
		}
		version = latest.MaxVersion("", pre)
		query = version
	default:
		if !semver.IsValid(query) {
			return fmt.Errorf("invalid version: %s", query)
		}
		// best effort to detect +incompatible versions
		if v := mod.MaxVersion(query, pre); v != "" {
			version = v
		} else {
			version = query
		}
	}
	// go get
	if goget {
		spec := packages.JoinPath(modprefix, version, pkgdir)
		if query != "" {
			spec += "@" + query
		}
		fmt.Println("go get", spec)
		cmd := exec.Command("go", "get", spec)
		cmd.Dir = dir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	// rewrite imports
	if !rewrite {
		return nil
	}
	return importpaths.RewriteModule(dir, importpaths.RewriteModuleOptions{
		PkgDir:     pkgdir,
		Prefix:     modprefix,
		NewVersion: version,
		OnRewrite: func(pos token.Position, _, newpath string) {
			fmt.Printf("%s %s\n", pos, newpath)
		},
	})
}

func pathcmd(args []string) error {
	var dir, version string
	var next, rewrite bool
	fset := flag.NewFlagSet("path", flag.ExitOnError)
	fset.BoolVar(&next, "next", false, "increment the module path version")
	fset.StringVar(&version, "version", "", "set the module path version")
	fset.BoolVar(&rewrite, "rewrite", true, "rewrite import paths")
	fset.StringVar(&dir, "dir", ".", "working directory")
	fset.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: gomajor path [modpath]")
		fset.PrintDefaults()
	}
	fset.Parse(args)
	// find and parse go.mod
	name, err := packages.FindModFile(dir)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	file, err := modfile.ParseLax(name, data, nil)
	if err != nil {
		return err
	}
	// figure out the new module path
	modpath := fset.Arg(0)
	if modpath == "" {
		modpath = file.Module.Mod.Path
	}
	// find the current version if one wasn't provided
	if version == "" {
		var ok bool
		version, ok = packages.ModMajor(modpath)
		if !ok || version == "" {
			version = "v1"
		}
	}
	// increment the path version
	if next {
		version, err = modproxy.NextMajor(version)
		if err != nil {
			return err
		}
	}
	if !semver.IsValid(version) {
		return fmt.Errorf("invalid version: %q", version)
	}
	// create the new modpath
	modprefix := packages.ModPrefix(modpath)
	oldmodprefix := packages.ModPrefix(file.Module.Mod.Path)
	modpath = packages.JoinPath(modprefix, version, "")
	fmt.Printf("module %s\n", modpath)
	if !rewrite {
		return nil
	}
	// update go.mod
	cmd := exec.Command("go", "mod", "edit", "-module", modpath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	// rewrite import paths
	return importpaths.RewriteModule(dir, importpaths.RewriteModuleOptions{
		Prefix:     oldmodprefix,
		NewVersion: version,
		NewPrefix:  modprefix,
		OnRewrite: func(pos token.Position, _, newpath string) {
			fmt.Printf("%s %s\n", pos, newpath)
		},
	})
}
