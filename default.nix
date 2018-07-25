{ stdenv, buildGoPackage, callPackage, makeWrapper }:

let
  vgo = callPackage ./vgo.nix {};

in buildGoPackage rec {
  name = "vgo2nix-${version}";
  version = "0.0.1";
  goPackagePath = "github.com/adisbladis/vgo2nix";

  nativeBuildInputs = [ makeWrapper ];

  src = ./.;
  goDeps = ./deps.nix;

  CGO_ENABLED = 0;

  postInstall = with stdenv; ''
    wrapProgram $bin/bin/vgo2nix --prefix PATH : ${lib.makeBinPath [ vgo ]}

  '';

}
