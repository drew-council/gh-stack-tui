{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    topiary-nushell = {
      url = "github:drew-council/topiary-nushell-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    treefmt-nix.url = "github:numtide/treefmt-nix";
  };
  outputs =
    inputs@{
      self,
      nixpkgs,
      flake-utils,
      topiary-nushell,
      treefmt-nix,
      ...
    }:
    # nixpkgs unstable no longer supports x86_64-darwin.
    flake-utils.lib.eachSystem [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ] (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
        };
        treefmtEval = treefmt-nix.lib.evalModule pkgs {
          imports = [
            topiary-nushell.treefmtModules.default
            ./treefmt.nix
          ];
        };
      in
      {
        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            (aspellWithDicts (ps: with ps; [ en ]))
            nushell
            go_1_27
            gopls
            golines
            gofumpt
            sqlc
          ];
        };

        packages = rec {
          ghst = (pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
            pname = "ghst";
            version = "0.1.0";
            src = ./.;
            nativeBuildInputs = [ pkgs.git ];
            vendorHash = "sha256-D6BKRpQZIfhRrepZDs6bpQmzr7maG0/ahh+pEkNo+Pc=";
            # The gh-stack shim is its own module; see third_party/gh-stack.
            excludedPackages = [ "third_party" ];
            postInstall = ''
              mv $out/bin/gh-stack-tui $out/bin/ghst
            '';
          };
          default = ghst;
        };

        formatter = treefmtEval.config.build.wrapper;
        checks.formatting = treefmtEval.config.build.check self;
        # The generated cache queries match queries.sql and schema.sql.
        checks.sqlc = pkgs.runCommand "sqlc-diff" { nativeBuildInputs = [ pkgs.sqlc ]; } ''
          cd ${./.}
          sqlc diff
          touch $out
        '';
      }
    );
}
