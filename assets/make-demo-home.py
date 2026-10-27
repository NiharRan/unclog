#!/usr/bin/env python3
"""Builds /tmp/unclog-demo-home, a throwaway home directory for recording assets/demo.gif.

Files are sparse (apparent size only), so a few GB of "cache" costs no real disk space,
and nothing in the recording comes from a real machine. macOS layout.
Usage, from the repo root:  make build && python3 assets/make-demo-home.py && vhs assets/demo.tape
"""
import os
import shutil
import time

HOME = "/tmp/unclog-demo-home"
now, day = time.time(), 86400

shutil.rmtree(HOME, ignore_errors=True)


def f(rel, size_mb, age_days):
    path = os.path.join(HOME, rel)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "wb") as fh:
        fh.truncate(int(size_mb * 1024 * 1024))
    t = now - age_days * day
    os.utime(path, (t, t))


f(".npm/_cacache/content-v2/sha512/aa/blob", 1450, 2)
f(".npm/_npx/9f3c/node_modules/pkg/index.js", 1620, 1)
f("Library/pnpm/store/v3/files/00/blob", 8200, 9)
f("Library/Caches/pip/http/blob", 340, 1)
f(".cache/uv/archive-v0/blob", 2300, 5)
f("Library/Caches/go-build/aa/blob", 610, 0.1)
f(".cache/huggingface/hub/models--meta-llama--Llama-3.1-8B-Instruct/blobs/b1", 15200, 34)
f(".cache/huggingface/hub/models--mistralai--Mistral-7B-v0.3/blobs/b1", 13900, 71)
f(".cache/huggingface/hub/datasets--squad/blobs/b1", 480, 120)
f(".ollama/models/blobs/sha256-demo", 4700, 12)
f(".claude/projects/-Users-dev-webapp/session-a.jsonl", 540, 3)
f(".claude/projects/-Users-dev-api-server/session-b.jsonl", 410, 21)
f(".claude/shell-snapshots/snapshot.sh", 12, 6)
for tool, age in (("Cursor", 40), ("Windsurf", 95)):
    base = f"Library/Application Support/{tool}"
    f(base + "/Cache/blob", 420 if tool == "Cursor" else 650, age)
    f(base + "/CachedData/blob", 90, age)
    f(base + "/CachedExtensionVSIXs/blob", 140, age)
    f(base + "/User/workspaceStorage/w/blob", 260, age)

os.makedirs(HOME + "/.config/unclog", exist_ok=True)
with open(HOME + "/.config/unclog/config.yaml", "w") as fh:
    fh.write("min_size: 100MB\n")  # hides the small leftovers so the table fits one screen
os.makedirs(HOME + "/bin", exist_ok=True)
shutil.copy("bin/unclog", HOME + "/bin/unclog")
print("demo home ready:", HOME)
