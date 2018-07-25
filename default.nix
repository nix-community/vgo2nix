{ stdenv, buildGoPackage }:

buildGoPackage rec {
  name = "vgo2nix-${version}";
  version = "0.0.1";
  goPackagePath = "github.com/adisbladis/vgo2nix";

  src = ./.;
  goDeps = ./deps.nix;

  CGO_ENABLED = 0;
}
