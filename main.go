package main // import "github.com/adisbladis/vgo2nix"

import (
	"encoding/json"
	"flag"
	"fmt"
	"golang.org/x/tools/go/vcs"
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

const depNixFormat = `
  {
    goPackagePath = "%s";
    fetch = {
      type = "%s";
      url = "%s";
      rev = "%s";
      sha256 = "%s";
    };
  }`

func getPackages(keepGoing bool) []*Package {
	var packages []*Package

	commitShaRev := regexp.MustCompile(`^v\d+\.\d+\.\d+-[0-9]{14}-(.*?)$`)
	commitRevV2 := regexp.MustCompile("^v.*-(.{12})\\+incompatible$")
	commitRevV3 := regexp.MustCompile(`^(v\d+\.\d+\.\d+)\+incompatible$`)

	modList, err := exec.Command("vgo", "list", "-m", "all").Output()
	if err != nil {
		panic(err)
	}
	// First line is always current module
	lines := strings.Split(string(modList), "\n")[1:]

	for _, line := range lines {
		if line == "" {
			continue
		}

		l := strings.SplitN(line, " ", 2)
		if len(l) != 2 {
			panic("Wrong length")
		}
		goPackagePath := l[0]
		revInfo := l[1]

		fmt.Println(fmt.Sprintf("Processing goPackagePath: %s", goPackagePath))

		repoRoot, err := vcs.RepoRootForImportPath(
			goPackagePath,
			true)
		if err != nil {
			panic(err)
		}

		rev := revInfo
		if commitShaRev.MatchString(rev) {
			rev = commitShaRev.FindAllStringSubmatch(rev, -1)[0][1]
		} else if commitRevV2.MatchString(rev) {
			rev = commitRevV2.FindAllStringSubmatch(rev, -1)[0][1]
		} else if commitRevV3.MatchString(rev) {
			rev = commitRevV3.FindAllStringSubmatch(rev, -1)[0][1]
		}

		fmt.Println(fmt.Sprintf("goPackagePath %s has rev %s", goPackagePath, rev))

		// Get sha256
		jsonOut, err := exec.Command(
			"nix-prefetch-git",
			"--quiet",
			"--url", repoRoot.Repo,
			"--rev", rev).Output()

		if err != nil {
			panic(err)
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(jsonOut, &resp); err != nil {
			panic(err)
		}
		sha256 := resp["sha256"].(string)

		if sha256 == "0sjjj9z1dhilhpc8pq4154czrb79z9cm044jvn75kxcjv6v5l2m5" {
			fmt.Println(fmt.Sprintf("Bad SHA256 for %s %s %s", goPackagePath, repoRoot.Repo, rev))

			if !keepGoing {
				panic("Exiting due to bad SHA256")
			}
		}

		pkg := &Package{
			GoPackagePath: goPackagePath,
			URL:           repoRoot.Repo,
			Rev:           rev,
			Sha256:        sha256,
		}
		packages = append(packages, pkg)
	}

	return packages
}

func main() {
	var keepGoing = flag.Bool("keep-going", false, "Whether to panic or not if a rev cannot be resolved (defaults to `false`)")
	flag.Parse()

	packages := getPackages(*keepGoing)

	outfile, err := os.Create("deps.nix")
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

	write("[")
	for _, pkg := range packages {
		write(fmt.Sprintf(depNixFormat,
			pkg.GoPackagePath, "git", pkg.URL,
			pkg.Rev, pkg.Sha256))
	}
	write("]")

	fmt.Println("Wrote deps.nix")

}
