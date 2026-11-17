--| tier: native
--| native: INSERT ... ON CONFLICT DO UPDATE ... WHERE with RETURNING old, so a first claim and a takeover are one statement that returns the token it replaced. Port: an engine must insert or conditionally update in one statement and report the replaced value (SQL Server has MERGE with OUTPUT deleted, SQLite has ON CONFLICT DO UPDATE but would read the old token first in the same transaction).
-- Claims key for token: inserts its row, claimed, when the key has none, or takes over a
-- claimed row whose lease ran out, on the database's clock. A takeover replaces the token,
-- restarts the lease, and keeps created_at, the first claim's time, and the row returned names
-- the token it replaced, whose log Claim then discards in the same transaction. A first claim
-- returns NULL. No row is returned, claiming nothing, when the key is recorded or another holds
-- an unexpired claim.
INSERT INTO docproc_entry (sha256, stage_name, stage_version, stage_config, token, expires_at, dispatch_version)
VALUES ({{sha256}}, {{stage_name}}, {{stage_version}}, {{stage_config}}, {{token}},
        now() + {{lease:interval}}, {{version}})
ON CONFLICT (sha256, stage_name, stage_version, stage_config) DO UPDATE
SET token = EXCLUDED.token, expires_at = EXCLUDED.expires_at,
    dispatch_version = EXCLUDED.dispatch_version
WHERE docproc_entry.entry IS NULL AND docproc_entry.expires_at <= now()
RETURNING old.token
