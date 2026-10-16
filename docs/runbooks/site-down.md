# Runbook: target is unreachable

Alert: `UplinkWatchSiteDown`

## Impact

The configured target has failed its complete reachability check for at least
five minutes from the named probe.

## Investigation

1. Open the local UplinkWatch dashboard and identify the failed stage.
2. Check whether other targets fail at the same time.
3. Inspect `check_failures_total` grouped by `stage` and `reason` in Grafana.
4. Compare the result from another network or an external probe if available.
5. For DNS failures, test the configured resolver and an independent resolver.
6. For TCP failures, verify the resolved address, destination port, firewall,
   and routing.
7. For TLS failures, inspect certificate validity, hostname, SNI, and system
   time.
8. For HTTP failures, inspect the status code and consider rate limiting,
   authentication, bot protection, and content changes.

## Mitigation

- Fix the local network or resolver when multiple unrelated targets fail.
- Contact the target owner when independent probes confirm the same failure.
- Adjust expected statuses or content only after confirming an intentional
  target-side change.
- Silence the alert during planned maintenance instead of weakening the rule.

## Resolution

Confirm that `site_reachable` has returned to `1` and remains stable for at
least two check intervals. Record the timeline and cause if the incident
consumed a meaningful part of the error budget.
