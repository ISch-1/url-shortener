-- Bootstrap only. Migrations proper live in internal/storage/postgres/migrations/
-- and are applied by the app on startup (see [ADR: TODO]).
CREATE EXTENSION IF NOT EXISTS pgcrypto;
