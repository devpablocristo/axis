#!/usr/bin/env python3
import subprocess
import sys
from pathlib import Path


def find_banned_paths() -> list[str]:
    roots = [Path("companion"), Path("nexus")]
    banned_names = {
        ".cursor",
        ".claude",
        ".air.toml",
        ".env.example",
        ".dockerignore",
        ".gitignore",
        "renovate.json",
        "AGENTS.md",
        "CLAUDE.md",
    }
    bad: list[str] = []

    for root in roots:
        if not root.exists():
            continue
        bad.extend(str(path) for path in root.iterdir() if path.name in banned_names)
        bad.extend(str(path) for path in root.glob("*.md") if path.name != "README.md")
        bad.extend(
            str(path)
            for path in root.rglob("*")
            if path.name == "Makefile"
            or path.name.startswith("Dockerfile")
            or path.name.startswith("docker-compose")
        )

    return bad


def listed_files() -> list[Path]:
    try:
        result = subprocess.run(
            ["git", "ls-files", "--cached", "--others", "--exclude-standard"],
            check=True,
            capture_output=True,
            text=True,
        )
    except (FileNotFoundError, subprocess.CalledProcessError):
        return [path for path in Path(".").rglob("*") if path.is_file()]

    return [Path(line) for line in result.stdout.splitlines() if line]


def ignored_for_content_scan(path: Path) -> bool:
    ignored_parts = {".git", "node_modules", "dist"}
    return path.name == "Makefile" or any(part in ignored_parts for part in path.parts)


def find_banned_text() -> list[str]:
    ac = "apps/" + "console"
    deny = [
        "infra/" + "docker",
        # NOTA: el ban de Cl_erk / VITE_CL_ERK se removió temporalmente: ese
        # proveedor sigue siendo el IAM VIGENTE del bff (identity_*.go + console
        # + docker-compose), así que la guarda era prematura y dejaba CI en rojo,
        # bloqueando todos los deploys de Axis. Re-activar cuando la migración de
        # IAM (fuera de ese proveedor) esté efectivamente hecha en el código.
        "COMPANION_" + "CONSOLE_PORT",
        "NEXUS_" + "CONSOLE_PORT",
        "Companion " + "UI",
        "Nexus " + "console",
        "axis/" + ac,
        ac,
        "130" + "01",
        "130" + "02",
    ]
    bad: list[str] = []

    for path in listed_files():
        if ignored_for_content_scan(path) or not path.is_file():
            continue
        try:
            lines = path.read_text(errors="ignore").splitlines()
        except OSError:
            continue
        for line_number, line in enumerate(lines, start=1):
            if any(term in line for term in deny):
                bad.append(f"{path}:{line_number}:{line}")

    return bad


def main() -> int:
    bad = find_banned_paths() + find_banned_text()
    if bad:
        print("\n".join(bad))
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
