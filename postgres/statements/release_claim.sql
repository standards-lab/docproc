--| tier: standard
-- Ends token's claim of key without a record: deletes the claimed row, whose log goes with it. A
-- recorded row, or a claim another token now holds, is left.
DELETE FROM docproc_entry
WHERE sha256 = {{sha256}} AND stage_name = {{stage_name}}
  AND stage_version = {{stage_version}} AND stage_config = {{stage_config}}
  AND token = {{token}} AND entry IS NULL
