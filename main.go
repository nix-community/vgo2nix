package main // import "github.com/adisbladis/vgo2nix"

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/tools/go/vcs"
)

type Package struct {
	GoPackagePath string
	URL           string
	Rev           string
	Sha256        string
	ModuleDir     string
}

type PackageResult struct {
	Package *Package
	Error   error
}

type modEntry struct {
	importPath string
	repo       string
	rev        string
	moduleDir  string
}

const depNixFormat = `  {
    goPackagePath = "%s";
    fetch = {
      type = "%s";
      url = "%s";
      rev = "%s";
      sha256 = "%s";
      moduleDir = "%s";
    };
  }`

var versionNumber = regexp.MustCompile(`^v\d+`)

func getModules() ([]*modEntry, error) {
	var entries []*modEntry

	var stderr bytes.Buffer
	cmd := exec.Command("go", "list", "-mod", "mod", "-json", "-m", "all")
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(),
		"GO111MODULE=on",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	type goModReplacement struct {
		Path    string
		Version string
	}

	type goMod struct {
		Path    string
		Main    bool
		Version string
		Replace *goModReplacement
	}

	var mods []goMod
	dec := json.NewDecoder(stdout)
	for {
		var mod goMod
		if err := dec.Decode(&mod); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		if !mod.Main {
			mods = append(mods, mod)
		}
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("'go list -m all' failed with %s:\n%s", err, stderr.String())
	}

	for _, mod := range mods {
		replacedPath := mod.Path
		version := mod.Version
		if mod.Replace != nil {
			replacedPath = mod.Replace.Path
			version = mod.Replace.Version
		}

		// find repo, and codeRoot
		repo, err := vcs.RepoRootForImportPath(replacedPath, false)
		if err != nil {
			return nil, err
		}

		// https://github.com/golang/go/blob/7bb6fed9b53494e9846689520b41b8e679bd121d/src/cmd/go/internal/modfetch/coderepo.go#L65
		pathPrefix := replacedPath
		if repo.Root != replacedPath {
			var ok bool
			pathPrefix, _, ok = module.SplitPathVersion(pathPrefix)
			if !ok {
				return nil, fmt.Errorf("invalid mod path: %s", replacedPath)
			}
		}

		// find submodule relative directory
		// https://github.com/golang/go/blob/7bb6fed9b53494e9846689520b41b8e679bd121d/src/cmd/go/internal/modfetch/coderepo.go#L74
		moduleDir := ""
		if pathPrefix != repo.Root {
			moduleDir = strings.TrimPrefix(pathPrefix, repo.Root+"/")
		}

		// convert version to git ref
		// https://github.com/golang/go/blob/7bb6fed9b53494e9846689520b41b8e679bd121d/src/cmd/go/internal/modfetch/coderepo.go#L656
		build := semver.Build(version) // +incompatible
		gitRef := strings.TrimSuffix(version, build)
		if strings.Count(gitRef, "-") >= 2 {
			// pseudo-version, use the commit hash
			gitRef = gitRef[strings.LastIndex(gitRef, "-")+1:]
		} else {
			if len(moduleDir) > 0 {
				// fix tag for submodule
				gitRef = moduleDir + "/" + gitRef
			}
		}

		fmt.Println(fmt.Sprintf("goPackagePath %s has rev: %s, module: %s", mod.Path, gitRef, moduleDir))
		entries = append(entries, &modEntry{
			importPath: mod.Path,
			repo:       repo.Repo,
			rev:        gitRef,
			moduleDir:  moduleDir,
		})
	}

	return entries, nil
}

func getPackages(keepGoing bool, numJobs int, prevDeps map[string]*Package) ([]*Package, error) {
	entries, err := getModules()
	if err != nil {
		return nil, err
	}

	processEntry := func(entry *modEntry) (*Package, error) {
		wrapError := func(err error) error {
			return fmt.Errorf("Error processing import path \"%s\": %v", entry.importPath, err)
		}

		if prevPkg, ok := prevDeps[entry.importPath]; ok {
			if prevPkg.URL == entry.repo && prevPkg.Rev == entry.rev {
				prevPkg.ModuleDir = entry.moduleDir
				return prevPkg, nil
			}
		}

		fmt.Println(fmt.Sprintf("Fetching %s %s", entry.importPath, entry.repo))
		// The options for nix-prefetch-git need to match how buildGoPackage
		// calls fetchgit:
		// https://github.com/NixOS/nixpkgs/blob/8d8e56824de52a0c7a64d2ad2c4ed75ed85f446a/pkgs/development/go-modules/generic/default.nix#L54-L56
		// and fetchgit's defaults:
		// https://github.com/NixOS/nixpkgs/blob/8d8e56824de52a0c7a64d2ad2c4ed75ed85f446a/pkgs/build-support/fetchgit/default.nix#L15-L23
		jsonOut, err := exec.Command(
			"nix-prefetch-git",
			"--quiet",
			"--fetch-submodules",
			"--no-deepClone",
			"--url", entry.repo,
			"--rev", entry.rev).Output()
		if err != nil {
			exitError, ok := err.(*exec.ExitError)
			if ok {
				return nil, wrapError(fmt.Errorf("nix-prefetch-git --fetch-submodules --no-deepClone --url %s --rev %s failed:\n%s",
					entry.repo,
					entry.rev,
					exitError.Stderr))
			}
			return nil, wrapError(fmt.Errorf("failed to execute nix-prefetch-git: %v", err))
		}
		fmt.Println(fmt.Sprintf("Finished fetching %s", entry.importPath))

		var resp map[string]interface{}
		if err := json.Unmarshal(jsonOut, &resp); err != nil {
			return nil, wrapError(err)
		}
		sha256 := resp["sha256"].(string)

		if sha256 == "0sjjj9z1dhilhpc8pq4154czrb79z9cm044jvn75kxcjv6v5l2m5" {
			return nil, wrapError(fmt.Errorf("Bad SHA256 for repo %s with rev: %s", entry.repo, entry.rev))
		}

		return &Package{
			GoPackagePath: entry.importPath,
			URL:           entry.repo,
			Rev:           entry.rev,
			Sha256:        sha256,
			ModuleDir:     entry.moduleDir,
		}, nil
	}

	worker := func(entries <-chan *modEntry, results chan<- *PackageResult) {
		for entry := range entries {
			pkg, err := processEntry(entry)
			result := &PackageResult{
				Package: pkg,
				Error:   err,
			}
			results <- result
		}
	}

	jobs := make(chan *modEntry, len(entries))
	results := make(chan *PackageResult, len(entries))
	for w := 1; w <= int(math.Min(float64(len(entries)), float64(numJobs))); w++ {
		go worker(jobs, results)
	}

	for _, entry := range entries {
		jobs <- entry
	}
	close(jobs)

	pkgsMap := make(map[string]*Package)
	for j := 1; j <= len(entries); j++ {
		result := <-results
		if result.Error != nil {
			if !keepGoing {
				return nil, result.Error
			}
			msg := fmt.Sprintf("Encountered error: %v", result.Error)
			fmt.Println(msg)
			continue
		}
		pkgsMap[result.Package.GoPackagePath] = result.Package
	}

	// Make output order stable
	var packages []*Package

	keys := make([]string, 0, len(pkgsMap))
	for k := range pkgsMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		packages = append(packages, pkgsMap[k])
	}

	return packages, nil
}

func main() {
	var keepGoing = flag.Bool("keep-going", false, "Whether to panic or not if a rev cannot be resolved (default \"false\")")
	var goDir = flag.String("dir", "./", "Go project directory")
	var out = flag.String("outfile", "deps.nix", "deps.nix output file (relative to project directory)")
	var in = flag.String("infile", "deps.nix", "deps.nix input file (relative to project directory)")
	var jobs = flag.Int("jobs", 20, "Number of parallel jobs")
	flag.Parse()

	err := os.Chdir(*goDir)
	if err != nil {
		panic(err)
	}

	// Load previous deps from deps.nix so we can reuse hashes for known revs
	prevDeps := loadDepsNix(*in)
	packages, err := getPackages(*keepGoing, *jobs, prevDeps)
	if err != nil {
		panic(err)
	}

	outfile, err := os.Create(*out)
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := outfile.Close(); err != nil {
			panic(err)
		}
	}()

	write := func(line string) {
		bytes := []byte(line + "\n")
		if _, err := outfile.Write(bytes); err != nil {
			panic(err)
		}
	}

	write("# file generated from go.mod using vgo2nix (https://github.com/adisbladis/vgo2nix)")
	write("[")
	for _, pkg := range packages {
		write(fmt.Sprintf(depNixFormat,
			pkg.GoPackagePath, "git", pkg.URL,
			pkg.Rev, pkg.Sha256, pkg.ModuleDir))
	}
	write("]")

	fmt.Println(fmt.Sprintf("Wrote %s", *out))
}
