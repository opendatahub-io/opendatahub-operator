-- Pass Rate Trend by Job (Daily)
-- Daily pass rate per job to spot degradation over time.
-- Use a wider date range (e.g., INTERVAL 30 DAY) for meaningful trends.
SELECT
  DATE(created) AS day,
  prowjob_job_name,
  COUNT(*) AS total_runs,
  COUNTIF(prowjob_state = 'success') AS passes,
  ROUND(COUNTIF(prowjob_state = 'success') / COUNT(*) * 100, 2) AS pass_rate_pct
FROM `openshift-gce-devel.ci_analysis_us.jobs`
WHERE prowjob_state IN ('success', 'failure')
  AND org = 'opendatahub-io'
  AND prowjob_start >= DATETIME(CURRENT_DATE()) AND prowjob_start < DATETIME_ADD(CURRENT_DATE(), INTERVAL 1 DAY)
GROUP BY day, prowjob_job_name
ORDER BY prowjob_job_name, day
LIMIT 1000