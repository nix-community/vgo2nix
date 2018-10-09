{ buildGoPackage, fetchFromGitHub }:

buildGoPackage rec {
  name = "vgo-${version}";
  version = "unstable-2018-09-11";

  goPackagePath = "golang.org/x/vgo";

  src = fetchFromGitHub {
    owner = "golang";
    repo = "vgo";
    rev = "9d567625acf4c5e156b9890bf6feb16eb9fa5c51";
    sha256 = "1g8m303zyha2hms7qpysi6w99lfwq2fai0vqj19m3m9lc6wylv57";
  };

  # Vgo needs access to compiler sources
  allowGoReference = true;
}
