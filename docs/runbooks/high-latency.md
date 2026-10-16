# Runbook: sustained high HTTP latency

Alert: `UplinkWatchHighLatency`

## Impact

The HTTP stage has exceeded 1.5 seconds for at least ten minutes. The target may
still be available, but user experience is degraded.

## Investigation

1. Compare DNS, TCP, TLS, and HTTP stage graphs in Grafana.
2. Check whether latency affects one target or all targets.
3. Compare results from another connection or external probe.
4. Look for packet loss, Wi-Fi congestion, VPN usage, or ISP degradation.
5. Check whether the target redirects, rate-limits, or serves a larger response.

## Mitigation

- Resolve local network contention when several unrelated targets slow down.
- Escalate to the target owner when the HTTP stage alone is slow from multiple
  networks.
- Tune the threshold for a target only after establishing its normal baseline.

## Resolution

Confirm latency remains below the threshold for multiple evaluation intervals.
Document the baseline change if the old threshold is no longer meaningful.
