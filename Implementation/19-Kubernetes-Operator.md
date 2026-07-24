# Kubernetes Operator

A Kubernetes Operator can make BoltRunner execution more cloud-native.

## Custom Resources

Potential custom resources include:

- `PerformanceTest`.
- `TestRun`.
- `LoadGeneratorPool`.
- `TestDataPool`.
- `ResultCollector`.

## Operator Responsibilities

- Reconcile desired test run state.
- Create and monitor jobs.
- Handle cleanup.
- Update custom resource status.
- Integrate with autoscaling.
- Enforce execution policies.

## Benefit

An operator allows BoltRunner to manage test execution declaratively, similar to how Kubernetes manages applications.
