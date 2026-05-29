-- GhostRoute PostgreSQL schema - Initial migration
-- Migrated from YellowTDS SQLite schema to PostgreSQL with UUID primary keys and JSONB columns

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE campaigns (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name            VARCHAR(255) NOT NULL,
    route           VARCHAR(512) NOT NULL UNIQUE,
    active          BOOLEAN DEFAULT true,
    flows           JSONB NOT NULL DEFAULT '[]',
    filters         JSONB NOT NULL DEFAULT '{}',
    settings        JSONB NOT NULL DEFAULT '{}',
    white_page_url  TEXT DEFAULT '',
    black_page_url  TEXT DEFAULT '',
    bot_threshold   FLOAT DEFAULT 0.7,
    geo_countries   TEXT[] DEFAULT '{}',
    allowed_devices TEXT[] DEFAULT '{}',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_campaigns_route ON campaigns(route);
CREATE INDEX idx_campaigns_active ON campaigns(active);
CREATE INDEX idx_campaigns_created_at ON campaigns(created_at);
