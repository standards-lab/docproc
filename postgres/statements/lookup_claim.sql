--| tier: standard
-- Reads key's row for a waiter: the dispatch.Version it was written under, its entry, NULL while
-- claimed, its lease's expiry, NULL once recorded or for a claim that never expires, and the
-- database's clock, so Wait times the lease on the clock that sets it. No row is returned when
-- the key is neither claimed nor recorded.
SELECT dispatch_version, entry, expires_at, CURRENT_TIMESTAMP
FROM docproc_entry
WHERE sha256 = {{sha256}} AND stage_name = {{stage_name}}
  AND stage_version = {{stage_version}} AND stage_config = {{stage_config}}
