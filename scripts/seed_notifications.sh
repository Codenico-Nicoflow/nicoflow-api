#!/usr/bin/env bash
# Seed a representative spread of notifications for one user, so the bell + panel
# can be eyeballed locally without the cron (which only fires at 08:00 local).
#
# Usage:
#   ./scripts/seed_notifications.sh                 # oldest user
#   ./scripts/seed_notifications.sh you@example.com # a specific user by email
#
# Runs against the docker-compose Postgres (service `postgres`, db/user `nicoflow`).
set -euo pipefail

EMAIL="${1:-}"
# Pick the target user: the given email, else the oldest account.
if [ -n "$EMAIL" ]; then
  USER_SELECT="SELECT id FROM users WHERE email = '${EMAIL}' LIMIT 1"
else
  USER_SELECT="SELECT id FROM users ORDER BY created_at LIMIT 1"
fi

# One row per interesting case: fresh unread, an already-read row (dims in the UI),
# a long title (truncation), and a markup title (proves the FE renders it as text).
docker compose exec -T postgres psql -U nicoflow -d nicoflow -v ON_ERROR_STOP=1 <<SQL
DO \$\$
DECLARE uid TEXT;
BEGIN
  ${USER_SELECT} INTO uid;
  IF uid IS NULL THEN
    RAISE EXCEPTION 'No matching user (register one first, or check the email)';
  END IF;

  INSERT INTO notifications (id, user_id, type, title, body, is_read, read_at, created_at) VALUES
    (gen_random_uuid()::text, uid, 'task_due_soon',       'Finish the Q3 report',           'This task is scheduled soon.', FALSE, NULL,   NOW()),
    (gen_random_uuid()::text, uid, 'task_due_soon',       'Call the dentist',               'This task is scheduled soon.', FALSE, NULL,   NOW() - INTERVAL '2 hours'),
    (gen_random_uuid()::text, uid, 'system_announcement', 'Welcome to Nicoflow',            'Thanks for trying it out.',    TRUE,  NOW(),  NOW() - INTERVAL '1 day'),
    (gen_random_uuid()::text, uid, 'task_due_soon',       'A very long task title that should truncate cleanly in the notification row without breaking the layout', 'This task is scheduled soon.', FALSE, NULL, NOW() - INTERVAL '30 minutes'),
    (gen_random_uuid()::text, uid, 'task_due_soon',       'Review <b>bold</b> & escaping',   'Title should render as literal text, not markup.', FALSE, NULL, NOW() - INTERVAL '5 minutes');

  RAISE NOTICE 'Seeded 5 notifications (4 unread) for user %', uid;
END
\$\$;
SQL

echo "Done. Reload the app (or wait <=60s for the bell poll) — badge should show 4."
