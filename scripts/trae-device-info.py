#!/usr/bin/env python3
"""Read Trae's registered device identifiers without exporting login tokens.

Run on the computer with the signed-in official client. Use --storage PATH for
a custom user-data directory's User/globalStorage/storage.json.
"""

import argparse
import json
import os
from pathlib import Path
import re
import sys


def storage_files(explicit):
    if explicit:
        yield Path(explicit).expanduser()
        return
    home = Path.home()
    roots = [home / "Library/Application Support",
             Path(os.environ.get("XDG_CONFIG_HOME", home / ".config"))]
    roots += [Path(os.environ[k]) for k in ("APPDATA", "LOCALAPPDATA")
              if os.environ.get(k)]
    seen = set()
    for root in roots:
        try:
            children = list(root.iterdir())
        except OSError:
            continue
        for child in children:
            if "trae" not in child.name.lower() or not child.is_dir():
                continue
            candidate = child / "User/globalStorage/storage.json"
            if candidate.is_file() and candidate not in seen:
                seen.add(candidate)
                yield candidate


def device_info(path):
    data = json.loads(path.read_text(encoding="utf-8-sig"))
    machine = data.get("telemetry.machineId", "")
    if not isinstance(machine, str):
        machine = ""
    devices = sorted({m.group(1) for key in data
                      if (m := re.fullmatch(r"iCubeAuthInfo://icube-dc:(\d{8,32})", key))})
    # Never serialize the source dictionary: it may contain account tokens.
    return [{"client": path.parents[2].name,
             "machine_id": machine, "device_id": device}
            for device in devices]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--storage", help="Path to the official client's storage.json")
    args = parser.parse_args()
    found = []
    for path in storage_files(args.storage):
        try:
            found.extend(device_info(path))
        except (OSError, ValueError, TypeError):
            print("Could not read a valid client storage file.", file=sys.stderr)
    if not found:
        print("No registered Trae device found. Run on the signed-in client's computer; "
              "use --storage for a custom data directory.", file=sys.stderr)
        return 1
    print(json.dumps(found, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
