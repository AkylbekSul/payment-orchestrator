-- Payment Orchestrator Database Schema
-- Version: 001
-- Description: Schema for Payment Orchestrator service

-- =====================================================
-- PAYMENT ORCHESTRATOR TABLES
-- =====================================================

-- Payment states (state machine)
CREATE TABLE IF NOT EXISTS payment_states (
    payment_id VARCHAR(255) PRIMARY KEY,
    state VARCHAR(50) NOT NULL,
    previous_state VARCHAR(50),
    fraud_decision VARCHAR(50),
    error_message TEXT,
    retry_count INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Composite index for state queries with time ordering
CREATE INDEX IF NOT EXISTS idx_payment_states_state_updated ON payment_states(state, updated_at DESC);

-- Outbox events (for reliable event publishing)
CREATE TABLE IF NOT EXISTS outbox_events (
    id BIGSERIAL PRIMARY KEY,
    aggregate_id VARCHAR(255) NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,
    published BOOLEAN DEFAULT FALSE,
    published_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Critical for outbox pattern - finding unpublished events
CREATE INDEX IF NOT EXISTS idx_outbox_events_unpublished ON outbox_events(created_at) 
    WHERE published = FALSE;
CREATE INDEX IF NOT EXISTS idx_outbox_events_aggregate_id ON outbox_events(aggregate_id);

-- Inbox events (for idempotent event processing)
CREATE TABLE IF NOT EXISTS inbox_events (
    id BIGSERIAL PRIMARY KEY,
    event_id VARCHAR(255) UNIQUE NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,
    processed BOOLEAN DEFAULT FALSE,
    processed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Partial index for unprocessed events
CREATE INDEX IF NOT EXISTS idx_inbox_events_unprocessed ON inbox_events(created_at) 
    WHERE processed = FALSE;

-- =====================================================
-- FUNCTIONS AND TRIGGERS
-- =====================================================

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Triggers for updated_at
CREATE TRIGGER update_payment_states_updated_at BEFORE UPDATE ON payment_states
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- =====================================================
-- COMMENTS
-- =====================================================

COMMENT ON TABLE payment_states IS 'State machine tracking for payment lifecycle';
COMMENT ON TABLE outbox_events IS 'Outbox pattern for reliable event publishing';
COMMENT ON TABLE inbox_events IS 'Inbox pattern for idempotent event processing';

-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_migrations (
    version VARCHAR(50) PRIMARY KEY,
    applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO schema_migrations (version) VALUES ('001_payment_orchestrator_schema') ON CONFLICT DO NOTHING;
