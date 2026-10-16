# UplinkWatch Windows scripts

- `Start Monitor.cmd` starts only the Windows agent and local web dashboard.
- `Start Observability.cmd` starts only Prometheus, Grafana, and Alertmanager in Docker.
- `Stop Observability.cmd` stops only the Docker services and preserves their data.
- `Start Everything.cmd` starts Docker services and the Windows agent together.
- `Stop Everything.cmd` stops the agent and Docker services while preserving stored metrics.
- `Refresh Sites from Browsers.cmd` replaces the single target list using Arc, Chrome, Edge, Brave, and Firefox.
- `Clear Site Lists.cmd` removes the generated target list so it can be rebuilt.

All command files are designed to be opened with a double-click.
