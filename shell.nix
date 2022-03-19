# I am a total noob at NIX. Shell is the only feature I use (regularly!) with
# simple and crappy scripts like this one. If you are a Nix ninja, please
# provide your input to improve this file.

# Why I use `nix-shell`?, because it builds and environment to develop this
# project without affecting any of my system packages, this implies not fucking
# up any of the 100 development environment i have in 100 folders on my system.

# This script installs Go (1.17) and IPFS (latest stable).

# Optionally, it sets some environment to ensure nothing is written outside of
# this folder (e.g., go packages, IPFS caches, ...)

# Optionally, it installs `delve` the Go debugger

# Optionally, it tweaks some system settings.
# (and if you don't like my settings, it's easy to add yours)

{ pkgs ? import <nixpkgs> {} }:
  pkgs.mkShell {
    nativeBuildInputs = [
      pkgs.buildPackages.go_1_17
      pkgs.buildPackages.ipfs
      pkgs.buildPackages.delve
    ];
    shellHook = ''
      # Store Go dependencies, IPFS node data, SH84 data and config
      # in this same folder rather than somewhere in HOME.
      export GOPATH=$PWD/.go
      export IPFS_PATH=$PWD/.ipfs-sh84
      export XDG_CACHE_HOME=$PWD/.xdg-cache
      export XDG_CONFIG_HOME=$PWD/.config
      # If IPFS is killing your router, setting this to false may help
      # See: https://github.com/mrusme/superhighway84/issues/14
      export ALLOW_QUIC_TRANSPORT=true

      echo "✅ Set environment:"
      echo "GOPATH=$GOPATH"
      echo "IPFS_PATH=$IPFS_PATH"
      echo "XDG_CACHE_HOME=$XDG_CACHE_HOME"
      echo "ALLOW_QUIC_TRANSPORT=$ALLOW_QUIC_TRANSPORT"
      echo "XDG_CONFIG_HOME=$XDG_CONFIG_HOME"

      echo "⏳  Building..."
      go build .

      # Bump (or reduce) file limit to a reasonable number for this shell
      ulimit -n 4096

      echo "⏳  IPFS init..."
      ipfs init
      ipfs config --json Swarm.Transports.Network.QUIC $ALLOW_QUIC_TRANSPORT

      echo "✅ Ready"
    '';
}
