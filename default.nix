{ pkgs ? import <nixpkgs> {} }:
with pkgs;

assert lib.versionAtLeast go.version "1.11";
buildGoPackage rec {
  name = "vgo2nix-${version}";
  version = "git";
  goPackagePath = "github.com/adisbladis/vgo2nix";

  nativeBuildInputs = [ makeWrapper ];

  src = lib.cleanSourceWith {
    filter = name: type: builtins.match ".*tests.*" name == null;
    src = (lib.cleanSource ./.);
  };

  goDeps = ./deps.nix;

  allowGoReference = true;

  postInstall = with stdenv; let
    binPath = lib.makeBinPath [ nix-prefetch-git go ];
  in ''
    wrapProgram $bin/bin/vgo2nix --prefix PATH : ${binPath}
  '';

  meta = with stdenv.lib; {
    description = "Convert go.mod files to nixpkgs buildGoPackage compatible deps.nix files";
    homepage = https://github.com/adisbladis/vgo2nix;
    license = licenses.mit;
    maintainers = with maintainers; [ adisbladis ];
  };

}
