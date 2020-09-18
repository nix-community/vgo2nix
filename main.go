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

	"golang.org/x/tools/go/vcs"
)

type Package struct {
	GoPackagePath string
	URL           string
	Rev           string
	Sha256        string
}

type PackageResult struct {
	Package *Package
	Error   error
}

type modEntry struct {
	importPath  string
	replacePath string
	rev         string
}

const depNixFormat = `  {
    goPackagePath = "%s";
    fetch = {
      type = "%s";
      url = "%s";
      rev = "%s";
      sha256 = "%s";
    };
  }`

var versionNumber = regexp.MustCompile(`^v\d+`)

func getModules() ([]*modEntry, error) {
	var entries []*modEntry

	commitShaRev := regexp.MustCompile(`^v\d+\.\d+\.\d+-(?:rc\d+\.)?(?:\d+\.)?[0-9]{14}-(.*?)(?:\+incompatible)?$`)
	commitRevV2 := regexp.MustCompile("^v.*-(.{12})\\+incompatible$")
	commitRevV3 := regexp.MustCompile(`^(v\d+\.\d+\.\d+)\+incompatible$`)

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

		if mod.Replace != nil {
			mod.Version = mod.Replace.Version
		}

		if !mod.Main {
			mods = append(mods, mod)
		}
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("'go list -m all' failed with %s:\n%s", err, stderr.String())
	}

	for _, mod := range mods {
		rev := mod.Version
		url, err := vcs.RepoRootForImportPath(mod.Path, false)
		if err != nil {
			return nil, err
		}

		if commitShaRev.MatchString(rev) {
			rev = commitShaRev.FindAllStringSubmatch(rev, -1)[0][1]
		} else if commitRevV2.MatchString(rev) {
			rev = commitRevV2.FindAllStringSubmatch(rev, -1)[0][1]
		} else if commitRevV3.MatchString(rev) {
			rev = commitRevV3.FindAllStringSubmatch(rev, -1)[0][1]
		} else if mod.Path != url.Root {
			subPath := strings.Split(mod.Path, url.Root+"/")[1]
			if !versionNumber.MatchString(subPath) {
				rev = subPath + "/" + rev
			}
		}

		replacePath := mod.Path
		if mod.Replace != nil {
			replacePath = mod.Replace.Path
		}
		fmt.Println(fmt.Sprintf("goPackagePath %s has rev: %s", mod.Path, rev))
		entries = append(entries, &modEntry{
			replacePath: replacePath,
			importPath:  mod.Path,
			rev:         rev,
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

		repoRoot, err := vcs.RepoRootForImportPath(
			entry.replacePath,
			false)
		if err != nil {
			return nil, wrapError(err)
		}

		importRoot, err := vcs.RepoRootForImportPath(
			entry.importPath,
			false)
		if err != nil {
			return nil, wrapError(err)
		}
		goPackagePath := importRoot.Root
		if versionNumber.MatchString(strings.TrimPrefix(entry.importPath, importRoot.Root+"/")) {
			goPackagePath = entry.importPath
		}

		if prevPkg, ok := prevDeps[goPackagePath]; ok {
			if prevPkg.Rev == entry.rev {
				return prevPkg, nil
			}
		}

		fmt.Println(fmt.Sprintf("Fetching %s", goPackagePath))
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
			"--url", repoRoot.Repo,
			"--rev", entry.rev).Output()
		if err != nil {
			exitError, ok := err.(*exec.ExitError)
			if ok {
				return nil, wrapError(fmt.Errorf("nix-prefetch-git --fetch-submodules --no-deepClone --url %s --rev %s failed:\n%s",
					repoRoot.Repo,
					entry.rev,
					exitError.Stderr))
			} else {

				return nil, wrapError(fmt.Errorf("failed to execute nix-prefetch-git: %v", err))
			}
		}
		fmt.Println(fmt.Sprintf("Finished fetching %s", goPackagePath))

		var resp map[string]interface{}
		if err := json.Unmarshal(jsonOut, &resp); err != nil {
			return nil, wrapError(err)
		}
		sha256 := resp["sha256"].(string)

		if sha256 == "0sjjj9z1dhilhpc8pq4154czrb79z9cm044jvn75kxcjv6v5l2m5" {
			return nil, wrapError(fmt.Errorf("Bad SHA256 for repo %s with rev: %s", repoRoot.Repo, entry.rev))
		}

		return &Package{
			GoPackagePath: goPackagePath,
			URL:           repoRoot.Repo,
			Rev:           entry.rev,
			Sha256:        sha256,
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
			pkg.Rev, pkg.Sha256))
	}
	write("]")

	fmt.Println(fmt.Sprintf("Wrote %s", *out))
}
