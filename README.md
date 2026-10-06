# webuntis-cli

Command line for [WebUntis](https://webuntis.com): timetable, changes, homework, exams, absences, school messages and school info, as text or, with `--json`, as JSON. `webuntis-cli mcp` exposes the same commands as an MCP server for AI assistants. Read-only.

Go port of [webuntis-mcp](https://github.com/toughIQ/webuntis-mcp). It uses the internal JSON-RPC API of the Untis Mobile app with a TOTP per request, so no password is needed.

## Install

```bash
go install .
nix profile install .   # or: nix build, nix run . -- today
```

`devenv shell` (or direnv) provides Go and goimports. After changing Go dependencies, run `nix run .#vendor-hash` to update `go.mod.sri`, the flake's vendor hash.

## Configuration

Values come from WebUntis: Profile > Freigaben > Zugriff über Untis Mobile. Environment variables take precedence over `~/.config/webuntis/env`.

| Key | Required | Meaning |
|---|---|---|
| `WEBUNTIS_SERVER` | yes | host, e.g. `yourschool.webuntis.com` |
| `WEBUNTIS_SCHOOL` | yes | school login name |
| `WEBUNTIS_USERNAME` | yes | login name |
| `WEBUNTIS_SECRET` | yes | key (Schlüssel) behind the QR code; treat like a password |
| `WEBUNTIS_STUDENT` | no | default child's first name; otherwise the first child |
| `WEBUNTIS_LOG_LEVEL` | no | `debug`, `info`, `warn` (default) or `error`; same as `--log-level` |

With several children on one account, `--child` (MCP: `child`) picks one by first name. Logs go to stderr; secrets are never logged.

## Usage

```bash
webuntis-cli                                   # command overview
webuntis-cli today
webuntis-cli tomorrow --child anna             # next school day
webuntis-cli timetable --start-date 2026-10-12 --end-date 2026-10-16
webuntis-cli changes --json | jq '.lessons[] | {date, start, subject, status}'
webuntis-cli class-timetable --class-name 5a   # classes lists names
webuntis-cli homework --days-ahead 7
webuntis-cli exams
webuntis-cli absences
webuntis-cli messages --date 2026-10-12
webuntis-cli school-info                       # class, periods, holidays
```

Untis restricts some methods per account type; e.g. student accounts get `WebUntis API error (-8509): no right` for homework and absences. Absences cover every child on the account.

## MCP

```bash
claude mcp add -s user webuntis -- ~/go/bin/webuntis-cli mcp
```

Tools: `check_login`, `list_children`, `get_timetable`, `get_today`, `get_tomorrow`, `get_changes`, `get_class_timetable`, `list_classes`, `get_homework`, `get_exams`, `get_absences`, `get_messages`, `get_school_info`.

### Remote (HTTP)

```bash
webuntis-cli mcp --http 100.64.0.1:8080
```

Streamable HTTP without authentication of its own: bind to a private interface only (e.g. the Tailscale IP), never publicly.

### NixOS

```nix
# with inputs.webuntis.nixosModules.default imported:
services.webuntis-cli = {
  enable = true;
  listen = "100.64.0.1:8080";
  environmentFile = "/run/secrets/webuntis"; # WEBUNTIS_SERVER=…, WEBUNTIS_SCHOOL=…, …
};
```
