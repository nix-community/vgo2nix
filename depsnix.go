package main

import (
	"encoding/json"
	"os/exec"
)

func loadDepsNix() map[string]*Package {
	ret := make(map[string]*Package)

	jsonOut, err := exec.Command(
		"nix-instantiate",
		"--eval",
		"--expr", "builtins.toJSON (import ./deps.nix)",
	).Output()
	if err != nil {
		return ret
	}

	var layer1JSON string
	if err := json.Unmarshal(jsonOut, &layer1JSON); err != nil {
		panic(err)
	}

	var layer2JSON []map[string]interface{}
	if err := json.Unmarshal([]byte(layer1JSON), &layer2JSON); err != nil {
		panic(err)
	}

	for _, pkg := range layer2JSON {
		goPackagePath := pkg["goPackagePath"].(string)
		fetch := pkg["fetch"].(map[string]interface{})
		ret[goPackagePath] = &Package{
			GoPackagePath: goPackagePath,
			URL: fetch["url"].(string),
			Rev: fetch["rev"].(string),
			Sha256: fetch["sha256"].(string),
		}
	}

	return ret
}
