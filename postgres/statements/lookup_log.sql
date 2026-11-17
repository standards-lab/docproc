--| tier: standard
-- Reads which log key's recorded entry holds: the token that appended it, with the
-- dispatch.Version its row was written under. Items reads it before its first page, to page
-- under that token, and after its last, to tell a log read whole from one whose entry was
-- replaced or dropped while it read.
SELECT dispatch_version, token
FROM docproc_entry
WHERE sha256 = {{sha256}} AND stage_name = {{stage_name}}
  AND stage_version = {{stage_version}} AND stage_config = {{stage_config}}
  AND entry IS NOT NULL
