with (import <nixpkgs> {});

let
  vgo = callPackage ./vgo.nix {};

  # Expose all bin outputs from `go` without
  # propagating their env vars
  #
  # This makes sure we can use gofmt with vgo
  goNoPropagation = runCommand "go-no-propagation" {} ''
    mkdir -p $out/bin
    for f in ${vgo.passthru.go}/bin/*; do
      ln -sf $f $out/bin/$(basename $f)
    done
  '';

in mkShell rec {
  buildInputs = [
    nix-prefetch-git
    vgo
    goNoPropagation
    darwin.apple_sdk.frameworks.Security
    jq
  ];
}
