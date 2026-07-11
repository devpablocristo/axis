#!/usr/bin/env sh

find_quality_tool() {
  tool="$1"
  if command -v "$tool" >/dev/null 2>&1; then
    command -v "$tool"
    return 0
  fi

  for candidate in "$HOME/.local/bin/$tool" "$HOME/go/bin/$tool"; do
    if [ -x "$candidate" ]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  printf 'Required quality tool is not installed: %s\n' "$tool" >&2
  return 1
}
