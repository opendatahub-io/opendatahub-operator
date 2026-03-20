-- Flake Rate by Job
-- Jobs that sometimes pass and sometimes fail within the time window.
-- High failure_pct with both passes and failures indicates flakiness.
SELECT
  prowjob_job_name,
  COUNT(*) AS total_runs,
  COUNTIF(prowjob_state = 'success') AS passes,
  COUNTIF(prowjob_state = 'failure') AS failures,
  ROUND(COUNTIF(prowjob_state = 'failure') / COUNT(*) * 100, 2) AS failure_pct
FROM `openshift-gce-devel.ci_analysis_us.jobs`
WHERE prowjob_state IN ('success', 'failure')
  AND org = 'opendatahub-io'
  AND prowjob_start >= DATETIME(CURRENT_DATE()) AND prowjob_start < DATETIME_ADD(CURRENT_DATE(), INTERVAL 1 DAY)
GROUP BY prowjob_job_name
HAVING passes > 0 AND failures > 0
ORDER BY failure_pct DESC
LIMIT 1000