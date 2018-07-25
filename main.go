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

func getPackages() []*Package {
	var packages []*Package

	commitShaRev, err := regexp.Compile("^v0.0.0-[0-9]{14}-(.*?)$")
	if err != nil {
		panic(err)
	}

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

		repoRoot, err := vcs.RepoRootForImportPath(
			goPackagePath,
			true)
		if err != nil {
			panic(err)
		}

		rev := revInfo
		if commitShaRev.MatchString(rev) {
			rev = commitShaRev.FindAllStringSubmatch(rev, -1)[0][1]
		}

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
			panic("Empty sha256")
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

	flag.Parse()

	packages := getPackages()

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
		write("  {")
		write(fmt.Sprintf("    goPackagePath = \"%s\";", pkg.GoPackagePath))
		write("    fetch = {")
		write("      type = \"git\";")
		write(fmt.Sprintf("      url = \"%s\";", pkg.URL))
		write(fmt.Sprintf("      rev = \"%s\";", pkg.Rev))
		write(fmt.Sprintf("      sha256 = \"%s\";", pkg.Sha256))
		write("    };")
		write("  }")

	}
	write("]")

}
