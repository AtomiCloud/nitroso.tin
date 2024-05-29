{ pkgs, atomi, pkgs-2305, pkgs-feb-23-24 }:
let
  all = {
    atomipkgs = (
      with atomi;
      {
        inherit
          infisical
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
    feb-23-24 = (
      with pkgs-feb-23-24;
      {
        inherit
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
feb-23-24 //
atomipkgs
