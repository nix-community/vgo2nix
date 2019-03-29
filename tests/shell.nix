with import <nixpkgs> {};

let
  vgo2nix = import ../default.nix {};

in mkShell {
  buildInputs = [
    python3
    vgo2nix
  ];
}
