#!/usr/bin/env python3
from __future__ import annotations

import argparse
import sys
from pathlib import Path
from typing import Any

try:
    import yaml
except ImportError as exc:
    print("PyYAML is required. Install with: python -m pip install PyYAML==6.0.2", file=sys.stderr)
    raise SystemExit(2) from exc

try:
    from openapi_spec_validator import validate_spec
except ImportError as exc:
    print(
        "openapi-spec-validator is required. Install with: "
        "python -m pip install openapi-spec-validator==0.7.2",
        file=sys.stderr,
    )
    raise SystemExit(2) from exc


MCP_PATHS = {
    "/mcp",
    "/v1/mcp/tools",
    "/v1/mcp/tools/call",
}

MCP_SCHEMAS = {
    "MCPJSONRPCRequest",
    "MCPJSONRPCResponse",
    "MCPTool",
    "MCPToolsResponse",
    "MCPToolCallRequest",
    "MCPToolCallResponse",
}


def main() -> int:
    parser = argparse.ArgumentParser(description="Validate Axis OpenAPI documents.")
    parser.add_argument("files", nargs="+", type=Path)
    args = parser.parse_args()

    failures: list[str] = []
    for path in args.files:
        failures.extend(validate_file(path))

    if failures:
        print("\n".join(failures), file=sys.stderr)
        return 1
    return 0


def validate_file(path: Path) -> list[str]:
    failures: list[str] = []
    try:
        raw = path.read_text()
    except OSError as exc:
        return [f"{path}: cannot read file: {exc}"]

    try:
        spec = yaml.safe_load(raw)
    except yaml.YAMLError as exc:
        return [f"{path}: invalid YAML: {exc}"]

    if not isinstance(spec, dict):
        return [f"{path}: OpenAPI document must be a YAML object"]

    version = str(spec.get("openapi", "")).strip()
    if not version.startswith("3."):
        failures.append(f"{path}: openapi must be a 3.x document, got {version!r}")

    try:
        validate_spec(spec)
    except Exception as exc:  # validator exceptions vary by installed version.
        failures.append(f"{path}: invalid OpenAPI spec: {exc}")

    if is_companion_openapi(path):
        failures.extend(validate_companion_mcp_contract(path, spec))

    if not failures:
        print(f"{path}: ok")
    return failures


def validate_companion_mcp_contract(path: Path, spec: dict[str, Any]) -> list[str]:
    failures: list[str] = []
    paths = spec.get("paths")
    if not isinstance(paths, dict):
        return [f"{path}: paths must be an object"]
    schemas = (((spec.get("components") or {}).get("schemas")) or {})
    if not isinstance(schemas, dict):
        return [f"{path}: components.schemas must be an object"]

    for required_path in sorted(MCP_PATHS):
        if required_path not in paths:
            failures.append(f"{path}: missing MCP path {required_path}")

    for required_schema in sorted(MCP_SCHEMAS):
        if required_schema not in schemas:
            failures.append(f"{path}: missing MCP schema {required_schema}")

    return failures


def is_companion_openapi(path: Path) -> bool:
    return path.as_posix().endswith("companion/openapi.yaml")


if __name__ == "__main__":
    raise SystemExit(main())
