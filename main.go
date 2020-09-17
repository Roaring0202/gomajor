package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/icholy/gomajor/importpaths"
	"github.com/icholy/gomajor/packages"
)

func main() {
	flag.Parse()
	if flag.NArg() != 1 {
		log.Fatal("missing package spec")
	}
	// figure out the correct import path
	pkgpath, version := packages.SplitSpec(flag.Arg(0))
	if !semver.IsValid(version) {
		log.Fatalf("invalid version: %s", version)
	}
	pkg, err := packages.Load(pkgpath)
	if err != nil {
		log.Fatal(err)
	}
	// go get
	cmd := exec.Command("go", "get", fmt.Sprintf("%s@%s", pkg.Path(version), version))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
	// rewrite imports
	err = importpaths.Rewrite(".", func(name, path string) (string, bool) {
		modpath, ok := pkg.FindModPath(path)
		if !ok {
			return "", false
		}
		pkgdir := strings.TrimPrefix(path, modpath)
		pkgdir = strings.TrimPrefix(pkgdir, "/")
		if pkg.PkgDir != "" && pkg.PkgDir != pkgdir {
			return "", false
		}
		return packages.Package{
			PkgDir:    pkgdir,
			ModPrefix: pkg.ModPrefix,
		}.Path(version), true
	})
	if err != nil {
		log.Fatal(err)
	}
}
