-- ROSETTA's role and database, with the limits the platform sets
-- (allspark apps/_template/database.sql). Local throwaway password only.
CREATE ROLE rosetta LOGIN CONNECTION LIMIT 6 PASSWORD 'rosetta';
ALTER ROLE rosetta SET statement_timeout = '30s';
ALTER ROLE rosetta SET idle_in_transaction_session_timeout = '60s';
ALTER ROLE rosetta SET temp_file_limit = '1GB';
CREATE DATABASE rosetta OWNER rosetta;
REVOKE ALL ON DATABASE rosetta FROM PUBLIC;
