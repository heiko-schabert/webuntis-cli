# NixOS module for webuntis-mcp. Import via the flake's nixosModules.default.
self:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.webuntis-mcp;
in
{
  options.services.webuntis-mcp = {
    enable = lib.mkEnableOption "WebUntis MCP server over HTTP";
    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
    };
    listen = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8080";
      description = "Address for Streamable HTTP.";
    };
    environmentFile = lib.mkOption {
      type = lib.types.path;
      description = "File with WEBUNTIS_SERVER, WEBUNTIS_SCHOOL, WEBUNTIS_USERNAME, WEBUNTIS_SECRET; kept out of the store.";
    };
    logLevel = lib.mkOption {
      type = lib.types.enum [
        "debug"
        "info"
        "warn"
        "error"
      ];
      default = "info";
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.services.webuntis-mcp = {
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      environment.WEBUNTIS_LOG_LEVEL = cfg.logLevel;
      serviceConfig = {
        ExecStart = "${lib.getExe cfg.package} mcp --http ${cfg.listen}";
        EnvironmentFile = cfg.environmentFile;
        DynamicUser = true;
        Restart = "on-failure";
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
      };
    };
  };
}
