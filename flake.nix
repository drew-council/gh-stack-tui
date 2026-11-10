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
        version = "0.1.0";
        # Every program under cmd, built and tested once.
        programs = (pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
          pname = "gh-stack-tui";
          inherit version;
          src = ./.;
          nativeBuildInputs = [ pkgs.git ];
          vendorHash = "sha256-6iZbI1DjuOHNs8lkVPqEJjVEDaLuCc9jSZClvNgkTzE=";
          # The gh-stack shim is its own module; see third_party/gh-stack.
          excludedPackages = [ "third_party" ];
        };
        # One of the programs, as a package of its own.
        program =
          name: description:
          pkgs.runCommand "${name}-${version}"
            {
              pname = name;
              inherit version;
              meta = {
                inherit description;
                mainProgram = name;
              };
            }
            ''
              install -D ${programs}/bin/${name} $out/bin/${name}
            '';
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
            # scripts/shot.nu
            tmux
            charm-freeze
          ];
          GHST_SHOT_FONT = "${pkgs.nerd-fonts.jetbrains-mono}/share/fonts/truetype/NerdFonts/JetBrainsMono/JetBrainsMonoNerdFontMono-Regular.ttf";
        };

        packages = rec {
          ghst = program "ghst" "A terminal UI for gh-stack stacks";
          ghrv = program "ghrv" "A terminal UI for the pull requests that wait on your review";
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
