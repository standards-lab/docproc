--| tier: standard
--| transaction: required
-- Discards the log a taken-over token appended, in the takeover's transaction, after the
-- claim's statement: a statement of its own, so it sees a flush that committed while the
-- takeover waited on the row's lock, and no item of the old token outlives the takeover.
DELETE FROM docproc_item
WHERE sha256 = {{sha256}} AND stage_name = {{stage_name}}
  AND stage_version = {{stage_version}} AND stage_config = {{stage_config}}
  AND token = {{token}}
