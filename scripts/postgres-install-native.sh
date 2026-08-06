#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only

set -eu

if ! command -v brew >/dev/null 2>&1; then
  echo "Homebrew is required to install native PostgreSQL 17" >&2
  exit 1
fi

brew install postgresql@17

postgres_prefix=$(brew --prefix postgresql@17)
pg_config="$postgres_prefix/bin/pg_config"
actual_sharedir="$postgres_prefix/share/postgresql"
actual_pkglibdir="$postgres_prefix/lib/postgresql"
compiled_sharedir=$("$pg_config" --sharedir)
compiled_pkglibdir=$("$pg_config" --pkglibdir)

ensure_formula_link() {
  expected_path=$1
  actual_path=$2
  if [ -e "$expected_path" ]; then
    return
  fi
  if [ ! -d "$actual_path" ]; then
    echo "PostgreSQL formula directory is missing: $actual_path" >&2
    exit 1
  fi
  mkdir -p "$(dirname "$expected_path")"
  ln -s "$actual_path" "$expected_path"
  echo "Repaired custom-prefix PostgreSQL path: $expected_path"
}

# Standard /opt/homebrew bottles already expose these paths. A custom Homebrew
# prefix may build from source and omit the versioned keg-only links expected by
# pg_config; repair only missing paths and never replace an existing entry.
ensure_formula_link "$compiled_sharedir" "$actual_sharedir"
ensure_formula_link "$compiled_pkglibdir" "$actual_pkglibdir"

echo "Native PostgreSQL $("$postgres_prefix/bin/postgres" --version) is installed"
