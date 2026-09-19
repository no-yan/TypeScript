import json
import os
import subprocess

ROOT = "/Volumes/SanDisk1TB/Library/Caches/ast-traversal-bench/e2-named-child-20260918"
RUN_ROOT = os.path.join(ROOT, "same-binary")
BEFORE = os.path.join(ROOT, "diagnostic")
AFTER = os.path.join(ROOT, "diagnostic")
CELLS = [
    "full-mixed-256",
    "full-mixed-16384",
    "expression-mixed-16384",
    "full-deep-4096",
]


def sample(cell, label, binary, pair, position):
    path = os.path.join(RUN_ROOT, cell, f"{label}-{pair:02d}-{position}.json")
    env = {**os.environ, "GOMAXPROCS": "1"}
    if label in ("after", "control-first", "control-second", "warmup-after"):
        env["AST_EXPERIMENT_BOUND_BINARY_CHILDREN"] = "1"
    else:
        env.pop("AST_EXPERIMENT_BOUND_BINARY_CHILDREN", None)
    proc = subprocess.run(
        [binary, "sample", "--config", os.path.join(ROOT, cell + ".json")],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
        env=env,
    )
    if proc.returncode != 0:
        raise RuntimeError((cell, label, pair, proc.returncode, proc.stderr.decode()))
    with open(path, "wb") as f:
        f.write(proc.stdout)
    data = json.loads(proc.stdout)
    return {
        "cell": cell,
        "pair": pair,
        "position": position,
        "label": label,
        "ns_per_op": data["ns_per_op"],
        "bytes_per_op": data["bytes_per_op"],
        "allocs_per_op": data["allocs_per_op"],
        "visits": data["visits"],
        "checksum": data["checksum"],
        "logical_nodes": data["logical_nodes"],
        "edge_reads": data["edge_reads"],
        "attribute_reads": data["attribute_reads"],
        "gc_cycles": data["gc_cycles"],
        "valid": data["valid"],
    }


rows = []
for cell in CELLS:
    os.makedirs(os.path.join(RUN_ROOT, cell), exist_ok=True)
    sample(cell, "warmup-before", BEFORE, 0, 1)
    sample(cell, "warmup-after", AFTER, 0, 2)
    for pair in range(1, 13):
        order = [("before", BEFORE), ("after", AFTER)] if pair % 2 else [("after", AFTER), ("before", BEFORE)]
        for position, (label, binary) in enumerate(order, 1):
            rows.append(sample(cell, label, binary, pair, position))
    for pair in range(1, 13):
        rows.append(sample(cell, "control-first", AFTER, pair, 1))
        rows.append(sample(cell, "control-second", AFTER, pair, 2))

with open(os.path.join(RUN_ROOT, "samples.json"), "w") as f:
    json.dump(rows, f, indent=2)

keys = list(rows[0])
with open(os.path.join(RUN_ROOT, "samples.tsv"), "w") as f:
    f.write("\t".join(keys) + "\n")
    for row in rows:
        f.write("\t".join(str(row[key]) for key in keys) + "\n")
