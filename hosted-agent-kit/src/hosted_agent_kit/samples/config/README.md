# Scheduler configuration reference

Every file here is a complete, valid scheduler configuration (check with
`hack validate src/hosted_agent_kit/samples/config/*.yaml`). Start with `minimal.yaml`, then read
`kitchen-sink.yaml` for every key.

| File | Shows |
|---|---|
| `minimal.yaml` | The smallest valid file |
| `stateful-and-stateless.yaml` | `defaults`, overrides, per-user sessions, adoption of unbound sessions |
| `scheduler-strategies.yaml` | The four strategies |
| `scheduler-profiles.yaml` | Weighted scores and filters |
| `custom-plugins.yaml` | A profile that uses plugins registered in Python |
| `queues.yaml` | Waiting, rejecting, and deep queues |
| `circuit-breaker.yaml` | Failure threshold, open time, half-open trials |
| `warm-pool.yaml` | Warm sessions and creation retries |
| `version-pinning.yaml` | Pinning an agent version and draining other versions |
| `all-protocols.yaml` | Responses and Invocations agents together |
| `telemetry.yaml` | Per-agent telemetry |
| `hack-settings.yaml` | The optional `hack:` runtime settings section |
| `azure-embedded.yaml` | The configuration inside an `azure.yaml` |
| `kitchen-sink.yaml` | Every key, with ranges and defaults |

Rules that apply to all of them: unknown and duplicate keys are errors; `mode: stateful` needs
`affinity: user` and `mode: stateless` needs `affinity: none` (the default follows the mode);
`min_warm_sessions` cannot exceed `max_sessions`; an enabled queue needs `max_depth` above zero.
