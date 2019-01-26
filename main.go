package main // import "github.com/adisbladis/vgo2nix"

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"golang.org/x/tools/go/vcs"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strings"
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

type packageListEntry struct {
	goPackagePath string
	rev           string
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

// extractPackages - Extract a list of packages and their revs
func extractPackages() ([]*packageListEntry, error) {
	var entries []*packageListEntry

	commitShaRev := regexp.MustCompile(`^v\d+\.\d+\.\d+-[0-9]{14}-(.*?)$`)
	commitRevV2 := regexp.MustCompile("^v.*-(.{12})\\+incompatible$")
	commitRevV3 := regexp.MustCompile(`^(v\d+\.\d+\.\d+)\+incompatible$`)

	cmd := exec.Command("go", "list", "-m", "all")
	cmd.Env = append(os.Environ(),
		"GO111MODULE=on",
	)
	var modList, stderr bytes.Buffer
	cmd.Stdout = &modList
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("'go list -m all' failed with %s:\n%s", err, stderr.String())
	}
	// First line is always current module
	lines := strings.Split(modList.String(), "\n")[1:]

	for _, line := range lines {
		if line == "" {
			continue
		}

		l := strings.Split(line, " ")
		var goPackagePath string
		var revInfo string
		if len(l) == 2 {
			goPackagePath = l[0]
			revInfo = l[1]
		} else if len(l) == 5 && l[2] == "=>" {
			goPackagePath = l[3]
			revInfo = l[4]
		} else {
			return nil, fmt.Errorf("Wrong length")
		}

		fmt.Println(fmt.Sprintf("Processing goPackagePath: %s", goPackagePath))

		rev := revInfo
		if commitShaRev.MatchString(rev) {
			rev = commitShaRev.FindAllStringSubmatch(rev, -1)[0][1]
		} else if commitRevV2.MatchString(rev) {
			rev = commitRevV2.FindAllStringSubmatch(rev, -1)[0][1]
		} else if commitRevV3.MatchString(rev) {
			rev = commitRevV3.FindAllStringSubmatch(rev, -1)[0][1]
		}

		fmt.Println(fmt.Sprintf("goPackagePath %s has rev %s", goPackagePath, rev))

		entries = append(entries, &packageListEntry{
			goPackagePath: goPackagePath,
			rev:           rev,
		})
	}

	return entries, nil
}

func getPackages(keepGoing bool, numJobs int, prevDeps map[string]*Package) ([]*Package, error) {
	entries, err := extractPackages()
	if err != nil {
		return nil, err
	}

	processEntry := func(entry *packageListEntry) (*Package, error) {
		goPackagePath := entry.goPackagePath
		rev := entry.rev

		if prevPkg, ok := prevDeps[goPackagePath]; ok {
			if prevPkg.Rev == rev {
				return prevPkg, nil
			}
		}

		repoRoot, err := vcs.RepoRootForImportPath(
			goPackagePath,
			true)
		if err != nil {
			return nil, err
		}

		fmt.Println(fmt.Sprintf("Fetching %s", goPackagePath))
		jsonOut, err := exec.Command(
			"nix-prefetch-git",
			"--quiet",
			"--url", repoRoot.Repo,
			"--rev", rev).Output()
		fmt.Println(fmt.Sprintf("Finished fetching %s", goPackagePath))

		if err != nil {
			return nil, err
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(jsonOut, &resp); err != nil {
			return nil, err
		}
		sha256 := resp["sha256"].(string)

		if sha256 == "0sjjj9z1dhilhpc8pq4154czrb79z9cm044jvn75kxcjv6v5l2m5" {
			fmt.Println(fmt.Sprintf("Bad SHA256 for %s %s %s", goPackagePath, repoRoot.Repo, rev))

			if !keepGoing {
				return nil, fmt.Errorf("Exiting due to bad SHA256")
			}
		}

		return &Package{
			GoPackagePath: goPackagePath,
			URL:           repoRoot.Repo,
			Rev:           rev,
			Sha256:        sha256,
		}, nil
	}

	worker := func(entries <-chan *packageListEntry, results chan<- *PackageResult) {
		for entry := range entries {
			pkg, err := processEntry(entry)
			result := &PackageResult{
				Package: pkg,
				Error:   err,
			}
			results <- result
		}
	}

	jobs := make(chan *packageListEntry, 100)
	results := make(chan *PackageResult, 100)
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
			return nil, result.Error
		}
		pkgsMap[result.Package.GoPackagePath] = result.Package
	}

	// Return packages in correct order
	var packages []*Package
	for _, entry := range entries {
		packages = append(packages, pkgsMap[entry.goPackagePath])
	}
	return packages, nil
}

func main() {
	var keepGoing = flag.Bool("keep-going", false, "Whether to panic or not if a rev cannot be resolved (default \"false\")")
	var goDir = flag.String("dir", "./", "Go project directory")
	var out = flag.String("outfile", "deps.nix", "deps.nix output file (relative to project directory)")
	var jobs = flag.Int("jobs", 20, "Number of parallel jobs")
	flag.Parse()

	// Go modules are not relying on GOPATH
	os.Unsetenv("GOPATH")

	err := os.Chdir(*goDir)
	if err != nil {
		panic(err)
	}

	// Load previous deps from deps.nix so we can reuse hashes for known revs
	prevDeps := loadDepsNix("./deps.nix")
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

	fmt.Println("Wrote deps.nix")

}
