# StageFlow workspace-first API examples

## Workspace

```json
{
  "id": "workspace-payments",
  "name": "Payments",
  "slug": "payments",
  "owner_team": "platform",
  "description": "Workspace for payment orchestration",
  "status": "active",
  "policy": {
    "allowed_hosts": ["payments.internal", "auth.internal"],
    "max_saved_requests": 100,
    "max_flows": 100,
    "max_steps_per_flow": 25,
    "max_request_body_bytes": 1048576,
    "default_timeout_ms": 5000,
    "max_run_duration_seconds": 300,
    "default_retry_policy": {
      "enabled": true,
      "max_attempts": 3,
      "backoff_strategy": "exponential",
      "initial_interval": "250ms",
      "max_interval": "2s",
      "retryable_status_codes": [429, 502, 503, 504]
    }
  }
}
```

## Saved request

```json
{
  "id": "create-session",
  "name": "Create session",
  "description": "Authenticate against the auth service",
  "request_spec": {
    "method": "POST",
    "url_template": "https://auth.internal/sessions",
    "headers": {
      "Authorization": "Bearer {{secret.auth_token}}",
      "Content-Type": "application/json"
    },
    "body_template": "{\"tenant\":\"{{workspace.vars.tenant_id}}\",\"user\":\"{{run.input.user_id}}\"}"
  }
}
```

## Flow with inline steps

```json
{
  "id": "checkout-inline",
  "name": "Checkout inline",
  "description": "Inline checkout flow",
  "status": "active",
  "steps": [
    {
      "id": "create-order",
      "order_index": 0,
      "name": "create-order",
      "request_spec": {
        "method": "POST",
        "url_template": "https://payments.internal/orders",
        "headers": {
          "Content-Type": "application/json"
        },
        "body_template": "{\"customer_id\":\"{{run.input.customer_id}}\"}"
      },
      "extraction_spec": {
        "rules": [
          {
            "name": "OrderID",
            "source": "body",
            "selector": "$.id",
            "required": true
          }
        ]
      },
      "assertion_spec": {
        "rules": [
          {
            "name": "created",
            "target": "status_code",
            "operator": "equals",
            "expected_value": "201"
          }
        ]
      }
    }
  ]
}
```

## Flow with saved request references

```json
{
  "id": "checkout-by-refs",
  "name": "Checkout by saved requests",
  "description": "Flow that references reusable saved requests",
  "status": "active",
  "steps": [
    {
      "id": "login-step",
      "order_index": 0,
      "name": "login-step",
      "step_type": "saved_request_ref",
      "saved_request_id": "create-session",
      "request_spec_override": {
        "headers": {
          "X-Trace-ID": "{{run.id}}"
        },
        "body_template": "{\"tenant\":\"{{workspace.vars.tenant_id}}\",\"user\":\"{{run.input.user_id}}\",\"scope\":\"checkout\"}"
      },
      "extraction_spec": {
        "rules": [
          {
            "name": "SessionToken",
            "source": "body",
            "selector": "$.token",
            "required": true
          }
        ]
      },
      "assertion_spec": {
        "rules": []
      }
    },
    {
      "id": "complete-checkout",
      "order_index": 1,
      "name": "complete-checkout",
      "request_spec": {
        "method": "POST",
        "url_template": "https://payments.internal/checkout/{{vars.SessionToken}}",
        "body_template": "{\"order_id\":\"{{run.input.order_id}}\"}"
      },
      "extraction_spec": {
        "rules": []
      },
      "assertion_spec": {
        "rules": [
          {
            "name": "accepted",
            "target": "status_code",
            "operator": "equals",
            "expected_value": "202"
          }
        ]
      }
    }
  ]
}
```

## Extraction rules with user-defined runtime vars

```json
{
  "rules": [
    {
      "name": "AccountID",
      "source": "body",
      "selector": "$.account.id",
      "required": true
    },
    {
      "name": "TenantSlug",
      "source": "header",
      "selector": "header.X-Tenant-Slug",
      "required": true
    },
    {
      "name": "StatusCode",
      "source": "status",
      "selector": "status_code",
      "required": true
    }
  ]
}
```

These values are available to later steps exactly as defined above, for example `{{vars.AccountID}}` and `{{vars.TenantSlug}}`.
