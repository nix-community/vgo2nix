with (import <nixpkgs> {});

let

  # Expose all bin outputs from `go` without
  # propagating their env vars
  #
  # This makes sure we can use gofmt with vgo
  goNoPropagation = runCommand "go-no-propagation" {} ''
    mkdir -p $out/bin
    for f in ${go}/bin/*; do
      ln -sf $f $out/bin/$(basename $f)
    done
  '';

in mkShell rec {
  buildInputs = [
    nix-prefetch-git
    goNoPropagation
  ] ++ stdenv.lib.optionals stdenv.isDarwin [
    darwin.apple_sdk.frameworks.Security
  ];
}
