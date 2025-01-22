{
  inputs = {
    # util
    flake-utils.url = "github:numtide/flake-utils";
    treefmt-nix.url = "github:numtide/treefmt-nix";
    pre-commit-hooks.url = "github:cachix/pre-commit-hooks.nix";

    # registry
    nixpkgs.url = "nixpkgs/d816b5ab44187a2dd84806630ce77a733724f95f";
    nixpkgs-2305.url = "nixpkgs/nixos-23.05";
    nixpkgs-240223.url = "nixpkgs/0e74ca98a74bc7270d28838369593635a5db3260";
    nixpkgs-240925.url = "nixpkgs/568bfef547c14ca438c56a0bece08b8bb2b71a9c";
    atomipkgs.url = "github:AtomiCloud/nix-registry/v1";
  };
  outputs =
    { self

      # utils
    , flake-utils
    , treefmt-nix
    , pre-commit-hooks

      # registries
    , atomipkgs
    , nixpkgs
    , nixpkgs-2305
    , nixpkgs-240223
    , nixpkgs-240925

    } @inputs:
    flake-utils.lib.eachDefaultSystem
      (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        pkgs-2305 = nixpkgs-2305.legacyPackages.${system};
        pkgs-240223 = nixpkgs-240223.legacyPackages.${system};
        pkgs-240925 = nixpkgs-240925.legacyPackages.${system};
        atomi = atomipkgs.packages.${system};
        pre-commit-lib = pre-commit-hooks.lib.${system};
      in
      let
        out = rec {
          pre-commit = import ./nix/pre-commit.nix {
            inherit pre-commit-lib formatter packages;
          };
          formatter = import ./nix/fmt.nix {
            inherit treefmt-nix pkgs;
          };
          packages = import ./nix/packages.nix {
            inherit pkgs atomi pkgs-2305 pkgs-240223 pkgs-240925;
          };
          env = import ./nix/env.nix {
            inherit pkgs packages;
          };
          devShells = import ./nix/shells.nix {
            inherit pkgs env packages;
            shellHook = checks.pre-commit-check.shellHook;
          };
          checks = {
            pre-commit-check = pre-commit;
            format = formatter;
          };
        };
      in
      with out;
      {
        inherit checks formatter packages devShells;
      }
      );
}
