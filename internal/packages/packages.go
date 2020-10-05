package packages

import (
	"path"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/tools/go/packages"
)

type Package struct {
	Version   string
	PkgDir    string
	ModPrefix string
}

func Direct(dir string) ([]*Package, error) {
	cfg := packages.Config{
		Mode:  packages.NeedModule,
		Dir:   dir,
		Tests: true,
	}
	pkgs, err := packages.Load(&cfg, "all")
	if err != nil {
		return nil, err
	}
	direct := []*Package{}
	seen := map[string]bool{}
	for _, pkg := range pkgs {
		if mod := pkg.Module; mod != nil && !mod.Indirect && mod.Replace == nil && !mod.Main && !seen[mod.Path] {
			seen[mod.Path] = true
			modprefix := pkg.Module.Path
			if prefix, _, ok := module.SplitPathVersion(modprefix); ok {
				modprefix = prefix
			}
			direct = append(direct, &Package{
				Version:   mod.Version,
				ModPrefix: modprefix,
			})
		}
	}
	return direct, nil
}

func (pkg Package) Incompatible() bool {
	return strings.Contains(pkg.Version, "+incompatible")
}

func (pkg Package) ModPath() string {
	if pkg.Incompatible() && !strings.HasPrefix(pkg.ModPrefix, "gopkg.in") {
		return pkg.ModPrefix
	}
	return JoinPathMajor(pkg.ModPrefix, semver.Major(pkg.Version))
}

func (pkg Package) Path() string {
	return path.Join(pkg.ModPath(), pkg.PkgDir)
}

func (pkg Package) FindModPath(pkgpath string) (string, bool) {
	if !strings.HasPrefix(pkgpath, pkg.ModPrefix) {
		return "", false
	}
	modpathlen := len(pkg.ModPrefix)
	if strings.HasPrefix(pkgpath[modpathlen:], "/") {
		modpathlen++
	}
	if idx := strings.Index(pkgpath[modpathlen:], "/"); idx >= 0 {
		modpathlen += idx
	} else {
		modpathlen = len(pkgpath)
	}
	if _, major, ok := module.SplitPathVersion(pkgpath[:modpathlen]); ok {
		return JoinPathMajor(pkg.ModPrefix, major), true
	}
	return pkg.ModPrefix, true
}

func SplitSpec(spec string) (path, version string) {
	parts := strings.SplitN(spec, "@", 2)
	if len(parts) == 2 {
		path = parts[0]
		version = parts[1]
	} else {
		path = spec
	}
	return
}

func JoinPathMajor(path, major string) string {
	if strings.HasPrefix(path, "gopkg.in") {
		major = strings.TrimPrefix(major, ".")
		return path + "." + major
	}
	major = strings.TrimPrefix(major, "/")
	if major == "v0" || major == "v1" || major == "" {
		return path
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path + major
}
