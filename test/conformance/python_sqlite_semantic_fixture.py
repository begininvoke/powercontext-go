"""Compare SQLite fixtures by logical schema and table contents."""

from __future__ import annotations

import argparse
import base64
import difflib
import json
import sqlite3
import sys
from pathlib import Path
from typing import Any


def encoded_cell(value: Any) -> Any:
    if isinstance(value, bytes):
        return {"base64": base64.b64encode(value).decode("ascii")}
    return value


def semantic_snapshot(database_path: Path) -> dict[str, Any]:
    if not database_path.is_file():
        raise FileNotFoundError(database_path)
    result: dict[str, Any] = {"schema_objects": [], "tables": {}}
    with sqlite3.connect(database_path) as connection:
        result["schema_objects"] = [
            {"type": row[0], "name": row[1], "table": row[2], "sql": row[3]}
            for row in connection.execute(
                "SELECT type, name, tbl_name, sql FROM sqlite_master "
                "WHERE name NOT LIKE 'sqlite_%' ORDER BY type, name, tbl_name, sql"
            )
        ]
        table_names = [
            row[0]
            for row in connection.execute(
                "SELECT name FROM sqlite_master "
                "WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name"
            )
        ]
        for table in table_names:
            quoted = '"' + table.replace('"', '""') + '"'
            columns = [row[1] for row in connection.execute(f"PRAGMA table_info({quoted})")]
            rows = [
                [encoded_cell(cell) for cell in row]
                for row in connection.execute(f"SELECT * FROM {quoted}")
            ]
            rows.sort(key=canonical_json)
            result["tables"][table] = {"columns": columns, "rows": rows}
    return result


def canonical_json(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))


def compare(expected_path: Path, actual_path: Path) -> None:
    expected = semantic_snapshot(expected_path)
    actual = semantic_snapshot(actual_path)
    if expected == actual:
        return
    expected_lines = json.dumps(expected, ensure_ascii=False, indent=2, sort_keys=True).splitlines()
    actual_lines = json.dumps(actual, ensure_ascii=False, indent=2, sort_keys=True).splitlines()
    difference = "\n".join(
        difflib.unified_diff(
            expected_lines,
            actual_lines,
            fromfile=str(expected_path),
            tofile=str(actual_path),
        )
    )
    raise ValueError(f"SQLite fixture semantics differ:\n{difference}")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("expected", type=Path)
    parser.add_argument("actual", type=Path)
    arguments = parser.parse_args()
    try:
        compare(arguments.expected, arguments.actual)
    except (FileNotFoundError, sqlite3.Error, ValueError) as error:
        print(error, file=sys.stderr)
        raise SystemExit(1) from error


if __name__ == "__main__":
    main()
