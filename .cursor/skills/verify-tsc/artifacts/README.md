# Verification artifacts

This directory holds transcripts from `control-tsc cli`. Each run uses a
subdirectory named after `VERIFY_TSC_RUN_ID`. Cleanup deletes scratch under
`/tmp/verify-tsc-*` and must leave these files in place.

Do not commit run transcripts. They are local proof for the agent that drove
the binary.
