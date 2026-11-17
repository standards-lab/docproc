--| tier: standard
-- Drops key's row, recorded or claimed, and with it every item of its log, so its content is
-- prepared again.
DELETE FROM docproc_entry
WHERE sha256 = {{sha256}} AND stage_name = {{stage_name}}
  AND stage_version = {{stage_version}} AND stage_config = {{stage_config}}
