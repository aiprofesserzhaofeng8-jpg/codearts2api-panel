# Ubuntu bubblewrap AppArmor profile

`bwrap-userns-restrict` is the unmodified profile distributed by Ubuntu's `apparmor-profiles` package, retrieved from the signed noble-updates/main package index during local verification. Its checksum is pinned in `docs/acceptance/CODEX-SANDBOX-FIX.json`.

This profile is distributed under GPL-2.0-or-later; see `LICENSE`. Package copyright notices are retained in `COPYRIGHT`. The profile permits user namespace creation for `/usr/bin/bwrap` with restricted child capabilities. Installing it requires an administrator and affects the local machine. `scripts/install-codex-sandbox.py` defaults to inspection only.

Official troubleshooting: https://learn.chatgpt.com/docs/sandboxing
