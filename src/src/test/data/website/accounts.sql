INSERT INTO public.users (username, display_name, password_hash, created_by_admin_id)
VALUES ('mock.writer', 'Mock Writer', $1, $3),
       ('mock.reader', 'Mock Reader', $2, $3)
ON CONFLICT DO NOTHING;
