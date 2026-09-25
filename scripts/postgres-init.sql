-- PostgreSQL initialization script for invoice-ocr-poc
-- This script runs on first container start

-- Create extensions if needed
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Performance tuning for current session
SET synchronous_commit = 'off';
SET work_mem = '64MB';

-- Note: The invoices table is auto-migrated by GORM.
-- This script just ensures extensions and initial settings are in place.

-- Optional: Create read-only user for analytics (uncomment if needed)
-- CREATE USER readonly_user WITH PASSWORD 'readonly_password';
-- GRANT CONNECT ON DATABASE invoice_ocr TO readonly_user;
-- GRANT USAGE ON SCHEMA public TO readonly_user;
-- GRANT SELECT ON ALL TABLES IN SCHEMA public TO readonly_user;
-- ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO readonly_user;
