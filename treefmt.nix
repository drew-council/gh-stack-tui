{
  ...
}:
{
  projectRootFile = "flake.nix";
  programs = {
    nixfmt = {
      enable = true;
    };
    jsonfmt = {
      enable = true;
    };
    shellcheck = {
      enable = true;
    };
    yamlfmt = {
      enable = true;
    };
    toml-sort = {
      enable = true;
    };
    dos2unix = {
      enable = true;
    };
    keep-sorted = {
      enable = true;
    };
    topiary-nushell = {
      enable = true;
    };
    golines = {
      enable = true;
    };
    gofumpt = {
      enable = true;
    };
  };

  settings = {
    # sqlc output; the sqlc flake check compares it with what sqlc generates.
    excludes = [ "internal/cache/cachedb/*" ];
    formatter = { };
  };
}
