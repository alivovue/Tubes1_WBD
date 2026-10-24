INSERT INTO public.access_groups (name, description)
VALUES ('Mock Editors', 'Sample employees with permission to edit the mock managed database'),
       ('Mock Readers', 'Sample employees with read-only access to the mock managed database')
ON CONFLICT DO NOTHING;
