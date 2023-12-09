{ pkgs, atomi, atomi_classic, pkgs-2305, pkgs-dec-06-23 }:
let
  all = {
    atomipkgs_classic = (
      with atomi_classic;
      {
        inherit
          sg;
      }
    );
    atomipkgs = (
      with atomi;
      {
        inherit
          infisical
          mirrord
          pls;
      }
    );
    nix-2305 = (
      with pkgs-2305;
      {
        inherit
          hadolint;
      }
    );
    dec-06-23 = (
      with pkgs-dec-06-23;
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

          # go
          go
          golangci-lint
          air

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
        nodejs = nodejs_20;
      }
    );
  };
in
with all;
atomipkgs //
atomipkgs_classic //
nix-2305 //
dec-06-23
