--| tier: standard
-- Renews token's claim of key for another lease from now, on the database's clock. No row is
-- affected when token no longer holds the claim: the key was taken over, released, or recorded.
UPDATE docproc_entry
SET expires_at = CURRENT_TIMESTAMP + {{lease:interval}}
WHERE sha256 = {{sha256}} AND stage_name = {{stage_name}}
  AND stage_version = {{stage_version}} AND stage_config = {{stage_config}}
  AND token = {{token}} AND entry IS NULL
