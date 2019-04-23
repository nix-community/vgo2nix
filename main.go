package main // import "github.com/adisbladis/vgo2nix"

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/tools/go/vcs"
)

type FlagStringArray []string

func (arr *FlagStringArray) String() string {
	return strings.Join(*arr, ", ")
}

func (arr *FlagStringArray) Set(v string) error {
	*arr = append(*arr, v)
	return nil
}

type Flags struct {
	keepGoing  *bool
	verbose    *bool
	index      *string
	projectDir *string
	rewrites   *FlagStringArray
}

type App struct {
	flags    Flags
	rewrites map[string]string
}

type Module struct {
	GoPackagePath string
	Rev           string
}
type Modules []*Module

type Package struct {
	GoPackagePath string
	URL           string
	Rev           string
	Sha256        string
}
type Packages []*Package
type PackageIndex map[string]*Package

const dependencyTemplate = `
  {
    goPackagePath = "%s";
    fetch = {
      type = "%s";
      url = "%s";
      rev = "%s";
      sha256 = "%s";
    };
  }
`

//

var pseudoVersionMatcher = regexp.MustCompile(`^v[0-9]+\.(0\.0-|\d+\.\d+-([^+]*\.)?0\.)\d{14}-[A-Za-z0-9]+(\+incompatible)?$`)

func isPseudoVersion(v string) bool {
	return strings.Count(v, "-") >= 2 && pseudoVersionMatcher.MatchString(v)
}

func pseudoVersionRev(v string) (string, error) {
	v = strings.TrimSuffix(v, "+incompatible")

	if isPseudoVersion(v) {
		_, rev, err := parsePseudoVersion(v)
		return rev, err
	}

	return v, nil
}

func parsePseudoVersion(v string) (string, string, error) {
	var (
		timestamp string
		rev       string
	)
	if !isPseudoVersion(v) {
		return "", "", fmt.Errorf("malformed pseudo-version %q", v)
	}
	j := strings.LastIndex(v, "-")
	v, rev = v[:j], v[j+1:]
	i := strings.LastIndex(v, "-")
	if j := strings.LastIndex(v, "."); j > i {
		timestamp = v[j+1:]
	} else {
		timestamp = v[i+1:]
	}
	return timestamp, rev, nil
}

//

func writeln(fd io.Writer, s string) error {
	_, err := fd.Write([]byte(s + "\n"))
	return err
}

func (app App) log(format string, v ...interface{}) {
	if *app.flags.verbose {
		log.Printf(format, v...)
	}
}

func (app App) readDependencies(path string) (PackageIndex, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		app.log("not reading dependencies from %s, file does not exists", path)
		return PackageIndex{}, nil
	}

	app.log("reading dependencies from %s", path)
	// this command will produce a string like "{\"key\": \"value\"}"
	// we need to unwrap it
	cmd := exec.Command(
		"nix-instantiate",
		"--eval",
		"--expr",
		fmt.Sprintf("builtins.toJSON (import %s)", path),
	)
	cmd.Stderr = os.Stderr
	buf, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return app.parseDependencies(buf)
}

func (app App) writeDependencies(path string, packages Packages) error {
	fd, err := os.Create(path)
	if err != nil {
		return err
	}
	defer fd.Close()

	err = writeln(fd, "# file generated from go.mod using vgo2nix (https://github.com/adisbladis/vgo2nix)")
	if err != nil {
		return err
	}

	err = writeln(fd, "[")
	if err != nil {
		return err
	}

	for _, pkg := range packages {
		err = writeln(fd, fmt.Sprintf(
			dependencyTemplate,
			pkg.GoPackagePath, "git", pkg.URL,
			pkg.Rev, pkg.Sha256,
		))
		if err != nil {
			return err
		}
	}

	return writeln(fd, "]")
}

func (app App) parseDependencies(buf []byte) (PackageIndex, error) {
	app.log("parsing dependencies")
	var (
		packages     = make(PackageIndex)
		unwrapped    string
		dependencies []map[string]interface{}
	)
	if err := json.Unmarshal(buf, &unwrapped); err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(unwrapped), &dependencies); err != nil {
		return nil, err
	}

	for _, pkg := range dependencies {
		goPackagePath := pkg["goPackagePath"].(string)
		fetch := pkg["fetch"].(map[string]interface{})
		packages[goPackagePath] = &Package{
			GoPackagePath: goPackagePath,
			URL:           fetch["url"].(string),
			Rev:           fetch["rev"].(string),
			Sha256:        fetch["sha256"].(string),
		}
	}

	return packages, nil
}

func (app App) readModules(root string) (Modules, error) {
	var buf bytes.Buffer

	cmd := exec.Command("go", "list", "-m", "all")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	cmd.Stdout = &buf
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return nil, err
	}

	// first line is always current module
	return app.parseModules(strings.Split(buf.String(), "\n")[1:])
}

func (app App) parseModules(lines []string) (Modules, error) {
	var (
		modules = make(Modules, len(lines))
		k       int
	)

	for _, line := range lines {
		if line == "" {
			continue
		}

		var (
			parts  = strings.Split(line, " ")
			module = &Module{}

			revInfo string
		)

		switch {
		case len(parts) == 2:
			module.GoPackagePath = parts[0]
			revInfo = parts[1]
		case len(parts) == 5 && parts[2] == "=>":
			module.GoPackagePath = parts[3]
			revInfo = parts[4]
		default:
			return nil, fmt.Errorf("has no parsing rules for module declaration '%s'", line)
		}

		rev, err := pseudoVersionRev(revInfo)
		if err != nil {
			return nil, fmt.Errorf("can not match rev in module declaration '%s' rev info '%s': %s", line, revInfo, err)
		}
		module.Rev = rev

		modules[k] = module
		k++
	}

	return modules[:k], nil
}

func (app App) prefetchModule(module *Module, rewrites map[string]string) (*Package, error) {
	var (
		buf map[string]interface{}
		url = module.GoPackagePath
	)
	if u, ok := rewrites[url]; ok {
		app.log("rewriting %s to %s", module.GoPackagePath, u)
		url = u
	}

	repoRoot, err := vcs.RepoRootForImportPath(url, *app.flags.verbose)
	if err != nil {
		return nil, err
	}

	args := []string{
		"--url", repoRoot.Repo,
		"--rev", module.Rev,
		"--fetch-submodules",
	}
	if !*app.flags.verbose {
		args = append(args, "--quiet")
	}

	app.log("prefetching repository with nix-prefetch-git %v", args)
	cmd := exec.Command(
		"nix-prefetch-git",
		args...,
	)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(out, &buf)
	if err != nil {
		return nil, err
	}
	sha256 := buf["sha256"].(string)

	// XXX: this magic hash indicates problems
	// see https://github.com/justinwoo/prefetch-github/blob/9c6dd8786d01f473569af3c6b775369f5ddfff41/prefetch-github.go#L18-L25
	if sha256 == "0sjjj9z1dhilhpc8pq4154czrb79z9cm044jvn75kxcjv6v5l2m5" {
		err = fmt.Errorf("bad SHA256 for package path %s, repo %s, rev %s", module.GoPackagePath, repoRoot.Repo, module.Rev)

		if *app.flags.keepGoing {
			log.Println(err)
		} else {
			return nil, err
		}
	}

	return &Package{
		GoPackagePath: module.GoPackagePath,
		URL:           repoRoot.Repo,
		Rev:           module.Rev,
		Sha256:        sha256,
	}, nil
}

func (app App) packagesFromModules(modules Modules, index PackageIndex) (Packages, error) {
	var (
		packages = make(Packages, len(modules))
		rewrites = app.processRewrites([]string(*app.flags.rewrites))
		err      error
	)

	for k, module := range modules {
		log.Printf("processing %s %s\n", module.GoPackagePath, module.Rev)

		pkg, ok := index[module.GoPackagePath]
		if ok && pkg.Rev != module.Rev {
			pkg = nil
		}

		if pkg == nil {
			pkg, err = app.prefetchModule(module, rewrites)
			if err != nil {
				return nil, err
			}
		}

		packages[k] = pkg
	}

	return packages, nil
}

func (app App) processRewrites(cskv []string) map[string]string {
	rewrites := map[string]string{}

	for _, rewrite := range cskv {
		fromto := strings.SplitN(rewrite, ":", 2)
		rewrites[fromto[0]] = fromto[1]
	}

	return rewrites
}

func (app App) Run() error {
	err := os.Chdir(*app.flags.projectDir)
	if err != nil {
		return err
	}

	indexPath, err := filepath.Abs(*app.flags.index)
	if err != nil {
		return err
	}
	projectDir, err := filepath.Abs(*app.flags.projectDir)
	if err != nil {
		return err
	}

	index, err := app.readDependencies(indexPath)
	if err != nil {
		return err
	}
	modules, err := app.readModules(projectDir)
	if err != nil {
		return err
	}
	packages, err := app.packagesFromModules(modules, index)
	if err != nil {
		return err
	}
	err = app.writeDependencies(indexPath, packages)
	if err != nil {
		return err
	}

	log.Printf("wrote %s", *app.flags.index)

	return nil
}

func main() {
	workDir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	rewrites := FlagStringArray{}
	flag.Var(&rewrites, "rewrite", "Rewrite remote address from:to")

	app := App{flags: Flags{
		keepGoing:  flag.Bool("keep-going", false, "Whether to fail or not if a rev cannot be resolved"),
		verbose:    flag.Bool("verbose", false, "Turn on verbose mode"),
		index:      flag.String("index", "deps.nix", "Nix depdendencies index path"),
		projectDir: flag.String("dir", workDir, "Project directory"),
		rewrites:   &rewrites,
	}}
	flag.Parse()

	err = app.Run()
	if err != nil {
		log.Fatal(err)
	}
}
