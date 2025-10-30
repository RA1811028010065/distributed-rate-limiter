# Sample logs

The files in this directory capture canonical outputs for the major validation flows. Use them as a reference when running the manual harnesses described in [`test file`](../test%20file).

- `runtime.log` – snapshot of the structured decision log emitted by the service. The file already contains a sample run; when you start the service it will append new decisions to the same file.
- `local-harness.log` – transcript of the local binary exercise, including stats queries.
- `compose-stack.log` – Docker Compose session showing request/response pairs and container logs.
- `ci-smoke.log` – condensed output from the automated Kind smoke test.

Each file can be diffed against your own session output to verify behaviour without needing a live cluster in this repository environment.
