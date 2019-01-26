package main

import (
	"github.com/orivej/go-nix/nix/eval"
	"github.com/orivej/go-nix/nix/parser"
	"log"
	"os"
)

func loadDepsNix(filePath string) map[string]*Package {
	ret := make(map[string]*Package)

	stat, err := os.Stat(filePath)
	if err != nil {
		return ret
	}
	if stat.Size() == 0 {
		return ret
	}

	p, err := parser.ParseFile(filePath)
	if err != nil {
		log.Println("Failed reading deps.nix")
		return ret
	}

	evalResult := eval.ParseResult(p)
	for _, pkgAttrsExpr := range evalResult.(eval.List) {
		pkgAttrs, ok := pkgAttrsExpr.Eval().(eval.Set)
		if !ok {
			continue
		}
		fetch, ok := pkgAttrs[eval.Intern("fetch")].Eval().(eval.Set)
		if !ok {
			continue
		}

		goPackagePath, ok := pkgAttrs[eval.Intern("goPackagePath")].Eval().(string)
		if !ok {
			continue
		}

		url, ok := fetch[eval.Intern("url")].Eval().(string)
		if !ok {
			continue
		}
		rev, ok := fetch[eval.Intern("rev")].Eval().(string)
		if !ok {
			continue
		}
		sha256, ok := fetch[eval.Intern("sha256")].Eval().(string)
		if !ok {
			continue
		}

		ret[goPackagePath] = &Package{
			GoPackagePath: goPackagePath,
			URL:           url,
			Rev:           rev,
			Sha256:        sha256,
		}
	}

	return ret
}
