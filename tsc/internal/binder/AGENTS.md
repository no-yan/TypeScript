# Binder performance investigations

For Store/index-based AST versus pointer-based AST binder investigations, read
`docs/binder-investigation-plan-20260910.md` before choosing the next experiment.
It records the investigation order the user asked to retain. Follow its sequence
and decision gates; update its progress when an investigation stage is completed.
This is investigation guidance, not an instruction to start benchmarks for
unrelated tasks.

For KPC measurement setup or reuse, start with
`docs/kpc-measurement-workflow.md`. It identifies the existing lifetime-safe
harness, counter self-tests, saved driver sources, and the planned shared CLI.
Check its implementation status before using any proposed command.
