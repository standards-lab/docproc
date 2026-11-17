--| tier: standard
-- Reads key's log, in append order: the items the recorded entry's token appended, which
-- Items streams. A claimed key's items are not yet a log, and an old token's are not the
-- entry's, so the join to the recorded row's token leaves both out.
SELECT i.item
FROM docproc_item i
JOIN docproc_entry e
  ON e.sha256 = i.sha256 AND e.stage_name = i.stage_name
 AND e.stage_version = i.stage_version AND e.stage_config = i.stage_config
 AND e.token = i.token
WHERE e.sha256 = {{sha256}} AND e.stage_name = {{stage_name}}
  AND e.stage_version = {{stage_version}} AND e.stage_config = {{stage_config}}
  AND e.entry IS NOT NULL
ORDER BY i.seq
