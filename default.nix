{ stdenv, lib, buildGoPackage, go, makeWrapper, nix-prefetch-git }:

assert lib.versionAtLeast go "1.11";
buildGoPackage rec {
  name = "vgo2nix-${version}";
  version = "0.0.1";
  goPackagePath = "github.com/adisbladis/vgo2nix";

  nativeBuildInputs = [ makeWrapper ];

  src = ./.;
  goDeps = ./deps.nix;

  CGO_ENABLED = 0;

  postInstall = with stdenv; let
    binPath = lib.makeBinPath [ nix-prefetch-git go ];
  in ''
    wrapProgram $bin/bin/vgo2nix --prefix PATH : ${binPath}
  '';

}
