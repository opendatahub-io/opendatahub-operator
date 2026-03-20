-- Pass Rate by Job Type (Presubmit vs Periodic vs Postsubmit)
-- Compare pass rates across job types to isolate where failures concentrate.
SELECT
  prowjob_type,
  COUNT(*) AS total_runs,
  COUNTIF(prowjob_state = 'success') AS passes,
  COUNTIF(prowjob_state = 'failure') AS failures,
  ROUND(COUNTIF(prowjob_state = 'success') / COUNT(*) * 100, 2) AS pass_rate_pct
FROM `openshift-gce-devel.ci_analysis_us.jobs`
WHERE prowjob_state IN ('success', 'failure')
  AND org = 'opendatahub-io'
  AND prowjob_start >= DATETIME(CURRENT_DATE()) AND prowjob_start < DATETIME_ADD(CURRENT_DATE(), INTERVAL 1 DAY)
GROUP BY prowjob_type
ORDER BY pass_rate_pct ASC
LIMIT 1000