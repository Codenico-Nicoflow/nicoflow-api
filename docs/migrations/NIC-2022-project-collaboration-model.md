# NIC-2022 Project collaboration model migration

Migration `054_project_collaboration_model` adds Project memberships and
invitations, lifecycle/version fields, and Task assignee/actor fields. It also
resets Notes as approved for the pre-launch product and makes every new Note
belong to a Project with cascading deletion.

## Before applying

1. Take and retain a PostgreSQL snapshot using the environment's standard
   backup process. The snapshot is the only way to recover Notes removed by
   this migration or reverse its data changes.
2. Record the Note count and confirm the target is the intended environment.
   This migration is approved only for the pre-launch data reset; do not apply
   it to production user Notes.
3. Test the migration against a disposable database and verify the schema and
   cascade behavior before rollout.

## Rollback

The `.down.sql` file restores the prior schema shape but cannot restore deleted
Note bodies. If rollback is required after applying `.up.sql`, stop writes,
restore the pre-migration snapshot, then roll application code back. Do not
expect `migrate down` alone to recover data. In production, follow the API
repository's roll-forward policy and do not run migrations backward.

The migration runner records the version only after the migration succeeds;
use its recorded version/dirty state to decide whether a retry is valid. Do not
manually re-execute individual statements against a partially migrated
database.
