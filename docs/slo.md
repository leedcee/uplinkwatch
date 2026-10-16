# Service level objective

UplinkWatch demonstrates an availability SLO for each configured target.

## Indicator

The availability SLI is the ratio of successful checks to all checks:

```text
successful checks / total checks
```

A check is successful only when DNS, TCP, TLS, HTTP status, and the optional
content assertion all satisfy the target configuration.

## Objective

- Objective: 99.9% successful checks
- Window: rolling 30 days
- Error budget: 0.1% of checks

At a one-minute check interval, a continuously configured target receives
approximately 43,200 checks in 30 days. The 0.1% error budget therefore permits
approximately 43 failed checks. This is a demonstration SLO and must be adjusted
to the real user impact and check frequency of a production service.

## Recording rules

`deploy/prometheus/rules.yml` calculates:

- `site:check_success:ratio5m`
- `site:check_success:ratio30d`
- `site:slo_error_ratio:ratio30d`
- `site:slo_error_budget_remaining:ratio30d`

Prometheus retention is configured for 30 days. The full-window value becomes
representative only after enough history has accumulated.

## Limitations

The agent observes availability from one network location. A target failure may
represent the target service, the local DNS resolver, the ISP, routing, or local
machine conditions. A second independent probe is required to distinguish these
causes with confidence.
