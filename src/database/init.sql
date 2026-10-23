-- Run with psql while connected to a maintenance database, such as postgres.
\set ON_ERROR_STOP on

CREATE DATABASE schakker_platform
    WITH TEMPLATE template0 ENCODING 'UTF8';

\connect schakker_platform
\ir schema.sql
