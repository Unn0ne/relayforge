CREATE TABLE endpoints (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    url text NOT NULL,
    secret_ciphertext bytea NOT NULL,
    timeout_ms integer NOT NULL DEFAULT 5000 CHECK (timeout_ms BETWEEN 100 AND 30000),
    max_attempts smallint NOT NULL DEFAULT 8 CHECK (max_attempts BETWEEN 1 AND 100),
    consecutive_failures integer NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    circuit_open_until timestamptz,
    disabled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE events (
    id uuid PRIMARY KEY,
    endpoint_id uuid NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    idempotency_key text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (endpoint_id, idempotency_key)
);

CREATE TABLE deliveries (
    id uuid PRIMARY KEY,
    event_id uuid NOT NULL UNIQUE REFERENCES events(id) ON DELETE CASCADE,
    endpoint_id uuid NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'retrying', 'succeeded', 'dead')),
    attempt_count smallint NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts smallint NOT NULL CHECK (max_attempts BETWEEN 1 AND 100),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    locked_by text,
    locked_until timestamptz,
    last_status_code integer CHECK (last_status_code BETWEEN 100 AND 599),
    last_error text NOT NULL DEFAULT '',
    last_completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((locked_by IS NULL) = (locked_until IS NULL)),
    CHECK ((status = 'processing') = (locked_by IS NOT NULL)),
    CHECK (attempt_count <= max_attempts)
);

CREATE TABLE delivery_attempts (
    id uuid PRIMARY KEY,
    delivery_id uuid NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
    attempt_number smallint NOT NULL CHECK (attempt_number > 0),
    status_code integer CHECK (status_code BETWEEN 100 AND 599),
    response_body text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    duration_ms integer NOT NULL CHECK (duration_ms >= 0),
    started_at timestamptz NOT NULL,
    completed_at timestamptz NOT NULL,
    UNIQUE (delivery_id, attempt_number),
    CHECK (completed_at >= started_at),
    CHECK (status_code IS NOT NULL OR error_message <> '')
);

CREATE INDEX deliveries_claim_idx
    ON deliveries (next_attempt_at, created_at)
    WHERE status IN ('pending', 'retrying', 'processing');

CREATE INDEX deliveries_endpoint_status_idx
    ON deliveries (endpoint_id, status, created_at DESC);

CREATE INDEX delivery_attempts_delivery_idx
    ON delivery_attempts (delivery_id, attempt_number DESC);
