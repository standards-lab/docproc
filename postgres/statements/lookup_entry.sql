--| tier: standard
-- Reads key's recorded entry, with the dispatch.Version its row was written under: Lookup's
-- read. A claimed key, whose entry is still NULL, has none, so its row is not returned.
SELECT dispatch_version, entry
FROM docproc_entry
WHERE sha256 = {{sha256}} AND stage_name = {{stage_name}}
  AND stage_version = {{stage_version}} AND stage_config = {{stage_config}}
  AND entry IS NOT NULL
