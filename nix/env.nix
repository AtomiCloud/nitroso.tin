{ pkgs, packages }:
with packages;
{
  system = [
    coreutils
    sd
    bash
    findutils
    jq
    yq-go
  ];

  dev = [
    skopeo
    pls
    git
    air
    doppler
    oapi-codegen
    curl
  ];

  infra = [
    helm
    kubectl
    docker
    k3d
    tilt
    mirrord
  ];

  main = [
    go
    infisical
  ];

  lint = [
    # core
    treefmt
    hadolint
    gitlint
    shellcheck
    helm-docs
    sg
    golangci-lint
  ];

  ci = [

  ];

  releaser = [
    sg
  ];

}
