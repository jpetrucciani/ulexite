{ pkgs ? import
    (fetchTarball {
      name = "jpetrucciani-2024-02-17";
      url = "https://github.com/jpetrucciani/nix/archive/77c36f3417767de48efc00a66503a781444df1d5.tar.gz";
      sha256 = "1qn5x45ac2mv7pklz5da78qq9ix4zq6r1y3qfn83r39km5fm90gd";
    })
    { }
}:
let
  name = "ulexite";

  inherit (pkgs.stdenv) isAarch64 isDarwin;
  isM1 = isDarwin && isAarch64;
  osSpecific = if isM1 then with pkgs.darwin.apple_sdk_11_0.frameworks; [CoreFoundation] else [];

  tools = with pkgs; {
    cli = [
      coreutils
      nixpkgs-fmt
    ];
    go = [
      clang
      go
      go-tools
      gopls
    ] ++ osSpecific;
    scripts = pkgs.lib.attrsets.attrValues scripts;
  };

  scripts = with pkgs; { };
  paths = pkgs.lib.flatten [ (builtins.attrValues tools) ];
  env = pkgs.buildEnv {
    inherit name paths; buildInputs = paths;
  };
in
(env.overrideAttrs (_: {
  inherit name;
  NIXUP = "0.0.6";
})) // { inherit scripts; }
