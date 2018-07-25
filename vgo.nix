{ buildGoPackage, fetchFromGitHub }:

buildGoPackage rec {
  name = "vgo-${version}";
  version = "unstable-2018-07-11";

  goPackagePath = "golang.org/x/vgo";

  src = fetchFromGitHub {
    owner = "golang";
    repo = "vgo";
    rev = "cc75ec08d5ecfc4072bcefc2c696d1c30af692b9";
    sha256 = "09bxrwnv2dcq6v9dmh1ydxd0cn6n4pilvglzakzq778xzbm1dgls";
  };

  # Vgo needs access to compiler sources
  allowGoReference = true;
}
