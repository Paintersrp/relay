# Local ChatGPT MCP registrations

Relay uses three ChatGPT MCP registrations, one per role app. The supported local commands supervise all three registrations together:

```bash
npm run chatgpt-mcp:init:all
npm run chatgpt-mcp:doctor:all
npm run chatgpt-mcp:start:all
npm run chatgpt-mcp:status:all
npm run chatgpt-mcp:stop:all
```

`start:all` is the normal daily startup command. The `*:all` commands are registration supervision only; they do not expose an additional MCP connector.

| Role | Relay route | Private ingress URL | Registration alias |
| --- | --- | --- | --- |
| Wayfinder | `http://127.0.0.1:18080/mcp/wayfinder` | `http://127.0.0.1:18101/mcp/wayfinder` | `relay-wayfinder` |
| Planner | `http://127.0.0.1:18080/mcp/planner` | `http://127.0.0.1:18102/mcp/planner` | `relay-planner` |
| Auditor | `http://127.0.0.1:18080/mcp/auditor` | `http://127.0.0.1:18103/mcp/auditor` | `relay-auditor` |

Each tunnel connects only to its role-specific private ingress listener. That listener forwards to one fixed role-app route and injects the bearer required by the protected Relay route. The tunnel client is not given the Relay bearer and cannot select another role or internal route.

The seven `/mcp/v1/...` values are internal route identities, not registration URLs. Connector setup uses only the three role-app URLs shown above.

For diagnostics, run `npm run chatgpt-mcp:doctor:all` before starting registrations. Use `status:all` to inspect the three registrations and `stop:all` to stop them.
