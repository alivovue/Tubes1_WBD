package setup

const RuntimeGrants = `
GRANT USAGE ON SCHEMA public TO {{role}};
GRANT SELECT,INSERT,UPDATE ON admin_users TO {{role}};
GRANT SELECT,INSERT,UPDATE,DELETE ON admin_sessions,admin_session_tokens TO {{role}};
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO {{role}};
`
