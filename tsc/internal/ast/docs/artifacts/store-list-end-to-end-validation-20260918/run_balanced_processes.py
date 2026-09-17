import hashlib
import json
import os
import time

OUT = "/Volumes/SanDisk1TB/Library/Caches/ast-traversal-bench/real-list-elision-20260918/monaco-balanced-warm"
BEFORE = "/Volumes/SanDisk1TB/Library/Caches/ast-traversal-bench/real-list-elision-20260918/tsgo-before"
AFTER = "/Volumes/SanDisk1TB/Library/Caches/ast-traversal-bench/real-list-elision-20260918/tsgo-after"
PROJECT = "/Volumes/SanDisk1TB/ghq/github.com/microsoft/vscode/src/tsconfig.monaco.json"
ARGS = ["-p", PROJECT, "--noEmit", "--noCheck", "--declaration", "false", "--checkers", "1"]

os.makedirs(OUT, exist_ok=True)
env = os.environ.copy()
env["GOMAXPROCS"] = "1"
env["GOMEMLIMIT"] = "2GiB"


def run(label, binary, pair, position):
    stem = os.path.join(OUT, f"{label}-{pair:02d}-{position}")
    stdout_path = stem + ".stdout"
    stderr_path = stem + ".stderr"
    pid = os.fork()
    if pid == 0:
        with open(os.devnull, "rb") as stdin, open(stdout_path, "wb") as stdout, open(stderr_path, "wb") as stderr:
            os.dup2(stdin.fileno(), 0)
            os.dup2(stdout.fileno(), 1)
            os.dup2(stderr.fileno(), 2)
            os.execve(binary, [binary, *ARGS], env)
    start = time.perf_counter_ns()
    _, status, usage = os.wait4(pid, 0)
    elapsed = time.perf_counter_ns() - start
    result = {
        "pair": pair,
        "position": position,
        "label": label,
        "elapsed_ns": elapsed,
        "user_s": usage.ru_utime,
        "system_s": usage.ru_stime,
        "max_rss_bytes": usage.ru_maxrss,
        "exit_code": os.waitstatus_to_exitcode(status),
        "stdout_sha256": hashlib.sha256(open(stdout_path, "rb").read()).hexdigest(),
        "stderr_sha256": hashlib.sha256(open(stderr_path, "rb").read()).hexdigest(),
    }
    return result


run("warmup-before", BEFORE, 0, 1)
run("warmup-after", AFTER, 0, 2)

rows = []
for pair in range(1, 13):
    order = [("before", BEFORE), ("after", AFTER)] if pair % 2 else [("after", AFTER), ("before", BEFORE)]
    for position, (label, binary) in enumerate(order, 1):
        rows.append(run(label, binary, pair, position))

for pair in range(1, 13):
    rows.append(run("control-first", AFTER, pair, 1))
    rows.append(run("control-second", AFTER, pair, 2))

with open(os.path.join(OUT, "samples.json"), "w") as f:
    json.dump(rows, f, indent=2)

with open(os.path.join(OUT, "samples.tsv"), "w") as f:
    f.write("pair\tposition\tlabel\telapsed_ns\tuser_s\tsystem_s\tmax_rss_bytes\texit_code\tstdout_sha256\tstderr_sha256\n")
    for row in rows:
        f.write("\t".join(str(row[key]) for key in ("pair", "position", "label", "elapsed_ns", "user_s", "system_s", "max_rss_bytes", "exit_code", "stdout_sha256", "stderr_sha256")) + "\n")
