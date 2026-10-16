# Runbook: agent metrics are missing

Alert: `UplinkWatchAgentMissing`

## Impact

Prometheus cannot scrape the UplinkWatch agent. Target availability is unknown;
this alert does not prove that the monitored sites are down.

## Investigation

1. Open `http://127.0.0.1:9876/healthz` on the Windows host.
2. Confirm that the UplinkWatch process is running.
3. Check the configured `listen_address` and whether port 9876 is occupied.
4. Confirm Docker can resolve `host.docker.internal`.
5. Check Windows Firewall rules if the agent listens on `0.0.0.0:9876`.
6. Inspect the Prometheus target page at `http://127.0.0.1:9090/targets`.

## Mitigation

- Restart the agent if it exited unexpectedly.
- Correct the scrape target or listen address.
- Restore Docker Desktop networking when the host gateway is unavailable.

## Resolution

Confirm the Prometheus target is `UP` and fresh `site_last_check_timestamp_seconds`
values are being collected.
