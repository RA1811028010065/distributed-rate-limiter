# Hybrid rate-limiting pattern compendium

This directory drills into every control technique that the distributed rate limiter can switch between at runtime. Each page
covers the mathematical model, when the controller activates it, diagnostics to watch, and how the synchronisation layer keeps
replicas aligned. The hybrid strategy blends three modern algorithms:

1. [Token bucket](token-bucket.md) for balanced, burst-friendly workloads.
2. [Leaky bucket](leaky-bucket.md) for absorbing sudden bursts and smoothing queue depth.
3. [Sliding window](sliding-window.md) for sustained throughput with fairness guarantees.

The [hybrid selector](hybrid-controller.md) page explains how real-time telemetry is turned into strategy decisions.

Use this compendium alongside `docs/product.md` when you need to explain *why* the service behaved a certain way under a given
traffic pattern. Each page also includes concrete experiments you can reproduce with `curl`, `hey`, or the automated smoke test.
