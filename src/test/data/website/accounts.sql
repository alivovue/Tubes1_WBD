INSERT INTO public.users (username, display_name, password_hash, is_platform_admin, is_active, created_by)
VALUES ('mock.writer', 'Mock Writer', $1, FALSE, TRUE, $3),
       ('mock.reader', 'Mock Reader', $2, FALSE, TRUE, $3)
ON CONFLICT DO NOTHING;
