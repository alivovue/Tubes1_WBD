INSERT INTO public.user_groups (user_id, group_id)
SELECT users.id, groups.id
FROM public.users AS users
JOIN public.access_groups AS groups
  ON (users.username = 'mock.writer' AND groups.name = 'Mock Editors')
  OR (users.username = 'mock.reader' AND groups.name = 'Mock Readers')
ON CONFLICT DO NOTHING;
