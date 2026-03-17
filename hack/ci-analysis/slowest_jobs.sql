-- Slowest Jobs (Average Duration)
-- Identify the longest-running jobs. Candidates for optimization or splitting.
SELECT
  prowjob_job_name,
  COUNT(*) AS total_runs,
  ROUND(AVG(DATETIME_DIFF(prowjob_completion, prowjob_start, SECOND)) / 60, 2) AS avg_duration_min,
  ROUND(MAX(DATETIME_DIFF(prowjob_completion, prowjob_start, SECOND)) / 60, 2) AS max_duration_min,
  ROUND(MIN(DATETIME_DIFF(prowjob_completion, prowjob_start, SECOND)) / 60, 2) AS min_duration_min
FROM `openshift-gce-devel.ci_analysis_us.jobs`
WHERE prowjob_state IN ('success', 'failure')
  AND org = 'opendatahub-io'
  AND prowjob_start IS NOT NULL
  AND prowjob_completion IS NOT NULL
  AND prowjob_completion >= prowjob_start
  AND prowjob_start >= DATETIME(CURRENT_DATE()) AND prowjob_start < DATETIME_ADD(CURRENT_DATE(), INTERVAL 1 DAY)
GROUP BY prowjob_job_name
ORDER BY avg_duration_min DESC
LIMIT 1000