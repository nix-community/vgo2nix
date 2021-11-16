with (import <nixpkgs> {});

mkShell rec {
  buildInputs = [
    nix-prefetch-git
    go
  ] ++ lib.optionals stdenv.isDarwin [
    darwin.apple_sdk.frameworks.Security
  ];

  shellHook = ''
    unset GOPATH
  '';
}
