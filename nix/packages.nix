{ pkgs, atomi, pkgs-2305, pkgs-240223, pkgs-240925 }:
let
  all = {
    atomipkgs = (
      with atomi;
      {
        inherit
          mirrord
          pls
          sg;
      }
    );
    nix-2305 = (
      with pkgs-2305;
      {
        inherit
          hadolint;
      }
    );
    nix-240925 = (
      with pkgs-240925;
      {
        inherit
          infisical;
      }
    );
    nix-240223 = (
      with pkgs-240223;
      {
        inherit
          gcc
          tilt
          skopeo
          doppler
          coreutils
          findutils
          sd
          bash
          git
          jq
          yq-go
          curl

          # go
          go
          golangci-lint
          air
          oapi-codegen

          # lint
          treefmt
          gitlint
          shellcheck
          helm-docs

          #infra
          kubectl
          docker
          k3d;
        helm = kubernetes-helm;
        npm = nodePackages.npm;
      }
    );
  };
in
with all;
nix-2305 //
nix-240223 //
nix-240925 //
atomipkgs
