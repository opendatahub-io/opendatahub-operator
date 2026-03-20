-- Duration by Pass/Fail Status
-- Compare duration of passing vs failing runs.
-- Failing faster than passing may indicate early crashes.
-- Failing slower than passing may indicate timeouts.
SELECT
  prowjob_job_name,
  prowjob_state,
  COUNT(*) AS runs,
  ROUND(AVG(DATETIME_DIFF(prowjob_completion, prowjob_start, SECOND)) / 60, 2) AS avg_duration_min,
  ROUND(MAX(DATETIME_DIFF(prowjob_completion, prowjob_start, SECOND)) / 60, 2) AS max_duration_min
FROM `openshift-gce-devel.ci_analysis_us.jobs`
WHERE prowjob_state IN ('success', 'failure')
  AND org = 'opendatahub-io'
  AND prowjob_start IS NOT NULL
  AND prowjob_completion IS NOT NULL
  AND prowjob_completion >= prowjob_start
  AND prowjob_start >= DATETIME(CURRENT_DATE()) AND prowjob_start < DATETIME_ADD(CURRENT_DATE(), INTERVAL 1 DAY)
GROUP BY prowjob_job_name, prowjob_state
ORDER BY prowjob_job_name, prowjob_state
LIMIT 1000