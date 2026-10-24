\set ON_ERROR_STOP on

CREATE DATABASE schakker_platform
    WITH TEMPLATE template0 ENCODING 'UTF8';

\connect schakker_platform
\ir schema.sql
